package dependencywiring

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kaeawc/grit/internal/catalog"
	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/downloader"
	"github.com/kaeawc/grit/internal/downloader/chain"
	"github.com/kaeawc/grit/internal/downloader/gradlecache"
	mavenread "github.com/kaeawc/grit/internal/downloader/mavenlocal"
	"github.com/kaeawc/grit/internal/downloader/mavenremote"
	"github.com/kaeawc/grit/internal/downloader/retry"
	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/lockfile/produce"
	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/m2localbridge"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/perf"
	mavenpublish "github.com/kaeawc/grit/internal/publish/mavenlocal"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/tieredcas"
	"github.com/kaeawc/grit/internal/transform/aarextract"
)

// ResolverCacheRoot returns the current production root for the legacy
// Gradle-cache-backed resolver source adapter.
func ResolverCacheRoot() string {
	return gradlecache.DefaultRoot()
}

// MaterializedRepositoryRoot is the local Maven-layout projection the
// dependency wiring writes from CAS for compiler consumption.
func MaterializedRepositoryRoot(workRoot string) string {
	if strings.TrimSpace(workRoot) == "" {
		return ""
	}
	return filepath.Join(workRoot, ".grit", "worktree", "materialized-m2")
}

// MaterializedAARRoot is the compatibility projection for extracted AAR
// outputs used by nativecompile.
func MaterializedAARRoot(workRoot string) string {
	if strings.TrimSpace(workRoot) == "" {
		return ""
	}
	return filepath.Join(workRoot, ".grit", "worktree", "aar")
}

func worktreeCASRoot(workRoot string) string {
	if strings.TrimSpace(workRoot) == "" {
		return ""
	}
	return filepath.Join(workRoot, ".grit", "worktree", "dependency-cas")
}

func sharedCASRoot() string {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".grit", "cas")
}

// CoordinateFromMaterializedPath parses dependency coordinates from the
// worktree materialization roots used by this package.
func CoordinateFromMaterializedPath(path string) (lockfile.Coordinate, bool) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	for _, marker := range []string{"/.grit/worktree/materialized-m2/", "/.grit/worktree/aar/"} {
		idx := strings.Index(clean, marker)
		if idx < 0 {
			continue
		}
		rest := strings.Split(strings.TrimPrefix(clean[idx+len(marker):], "/"), "/")
		if len(rest) < 4 {
			return lockfile.Coordinate{}, false
		}
		groupParts := rest[:len(rest)-3]
		if len(groupParts) == 0 {
			return lockfile.Coordinate{}, false
		}
		return lockfile.Coordinate{
			Group:    strings.Join(groupParts, "."),
			Artifact: rest[len(rest)-3],
			Version:  rest[len(rest)-2],
		}, true
	}
	return lockfile.Coordinate{}, false
}

// LoadCatalog returns the effective version catalog for prj, tolerating
// missing catalog files so read-only service surfaces can still function
// against partially materialized fixtures.
func LoadCatalog(prj *project.Project) (*catalog.Catalog, error) {
	if prj == nil || len(prj.VersionCatalogs) == 0 {
		return emptyCatalog(), nil
	}
	existing := make([]string, 0, len(prj.VersionCatalogs))
	for _, path := range prj.VersionCatalogs {
		if _, err := os.Stat(path); err == nil {
			existing = append(existing, path)
		}
	}
	if len(existing) == 0 {
		return emptyCatalog(), nil
	}
	return catalog.LoadAll(existing)
}

// DependencyResolver is the production-facing resolver surface consumed by
// nativecompile. The implementation is intentionally narrower than
// *m2local.Resolver so the dependency wiring can swap materialization
// strategies without dragging compiler call sites through those details.
type DependencyResolver interface {
	Resolve(*modulebuild.Dependencies) (*m2local.Resolved, error)
	SetTracker(perf.Tracker)
	Topology() m2local.CacheTopology
}

type legacyResolver interface {
	Resolve(*modulebuild.Dependencies) (*m2local.Resolved, error)
	SetTracker(perf.Tracker)
}

type resolvedMaterializer interface {
	Materialize(context.Context, *m2local.Resolved) (*m2local.Resolved, error)
}

type wiredResolver struct {
	legacy      legacyResolver
	materialize resolvedMaterializer
	topology    m2local.CacheTopology
}

func (r *wiredResolver) Resolve(deps *modulebuild.Dependencies) (*m2local.Resolved, error) {
	resolved, err := r.legacy.Resolve(deps)
	if err != nil || resolved == nil {
		return resolved, err
	}
	return r.materialize.Materialize(context.Background(), resolved)
}

func (r *wiredResolver) SetTracker(tracker perf.Tracker) {
	if r == nil || r.legacy == nil {
		return
	}
	r.legacy.SetTracker(tracker)
}

func (r *wiredResolver) Topology() m2local.CacheTopology {
	if r == nil {
		return m2local.CacheTopology{}
	}
	return r.topology
}

// Resolver constructs the production dependency resolver for prj.
func Resolver(prj *project.Project, tracker perf.Tracker) (DependencyResolver, error) {
	if prj == nil {
		return nil, os.ErrInvalid
	}
	cat, err := LoadCatalog(prj)
	if err != nil {
		return nil, err
	}
	legacy := m2local.New(ResolverCacheRoot(), prj.RootDir, prj.Repositories, cat)
	legacy.SetTracker(tracker)
	return &wiredResolver{
		legacy:      legacy,
		materialize: newStackMaterializer(prj),
		topology:    cacheTopology(prj),
	}, nil
}

// LoadCachedResolvedProduct reads the persisted resolved-dependency product
// using the production resolver wiring for prj.
func LoadCachedResolvedProduct(prj *project.Project, deps *modulebuild.Dependencies) (m2local.CachedResolvedProduct, error) {
	if prj == nil {
		return m2local.CachedResolvedProduct{}, os.ErrInvalid
	}
	cat, err := LoadCatalog(prj)
	if err != nil {
		return m2local.CachedResolvedProduct{}, err
	}
	product, err := m2local.LoadCachedResolvedProduct(ResolverCacheRoot(), prj.RootDir, prj.Repositories, cat, deps)
	if err != nil {
		return m2local.CachedResolvedProduct{}, err
	}
	topology := cacheTopology(prj)
	product.Topology = topology
	product.Inputs.Topology = topology
	return product, nil
}

// CacheTopology returns the resolver topology visible to current production
// call sites.
func CacheTopology(prj *project.Project) (m2local.CacheTopology, error) {
	if prj == nil {
		return m2local.CacheTopology{}, os.ErrInvalid
	}
	return cacheTopology(prj), nil
}

func emptyCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		Versions:  map[string]string{},
		Libraries: map[string]catalog.Library{},
		Bundles:   map[string][]string{},
	}
}

func cacheTopology(prj *project.Project) m2local.CacheTopology {
	topology := m2local.CacheTopology{
		SchemaVersion:        1,
		CacheRoot:            ResolverCacheRoot(),
		WorkRoot:             prj.RootDir,
		WorkMetadataRoot:     filepath.Join(prj.RootDir, ".grit", "metadata"),
		SharedMachineRoot:    sharedCASRoot(),
		SharedResolutionRoot: MaterializedRepositoryRoot(prj.RootDir),
		SharedAARRoot:        MaterializedAARRoot(prj.RootDir),
	}
	if root := mavenread.DefaultRoot(); root != "" {
		topology.Layers = append(topology.Layers, m2local.CacheLayer{
			Name:    "maven-local-source",
			Root:    root,
			Scope:   "machine",
			Content: "maven-local-layout",
			Shared:  true,
		})
	}
	if topology.CacheRoot != "" {
		topology.Layers = append(topology.Layers, m2local.CacheLayer{
			Name:    "gradle-cache-source",
			Root:    topology.CacheRoot,
			Scope:   "machine",
			Content: "gradle-cache-layout",
			Shared:  true,
		})
	}
	if root := worktreeCASRoot(prj.RootDir); root != "" {
		topology.Layers = append(topology.Layers, m2local.CacheLayer{
			Name:    "worktree-cas",
			Root:    root,
			Scope:   "worktree",
			Content: "content-addressed-blobs",
			Shared:  false,
		})
	}
	if topology.SharedMachineRoot != "" {
		topology.Layers = append(topology.Layers, m2local.CacheLayer{
			Name:    "shared-cas",
			Root:    topology.SharedMachineRoot,
			Scope:   "machine",
			Content: "content-addressed-blobs",
			Shared:  true,
		})
	}
	if topology.SharedResolutionRoot != "" {
		topology.Layers = append(topology.Layers, m2local.CacheLayer{
			Name:    "materialized-maven-local",
			Root:    topology.SharedResolutionRoot,
			Scope:   "worktree",
			Content: "local-materialization",
			Shared:  false,
		})
	}
	if topology.SharedAARRoot != "" {
		topology.Layers = append(topology.Layers, m2local.CacheLayer{
			Name:    "materialized-aar",
			Root:    topology.SharedAARRoot,
			Scope:   "worktree",
			Content: "aar-extraction",
			Shared:  false,
		})
	}
	return topology
}

type stackMaterializer struct {
	workRoot         string
	cacheRoot        string
	repositories     []project.Repository
	store            cas.Store
	downloader       downloader.Downloader
	repositoryRoot   string
	androidAARRoot   string
}

func newStackMaterializer(prj *project.Project) *stackMaterializer {
	worktreeStore := cas.NewFilesystemStore(worktreeCASRoot(prj.RootDir))
	tiers := []cas.Store{worktreeStore}
	if sharedRoot := sharedCASRoot(); sharedRoot != "" {
		tiers = append(tiers, cas.NewFilesystemStore(sharedRoot))
	}
	store, err := tieredcas.New(tiers...)
	if err != nil {
		store = nil
	}
	chainDownloader, err := chain.New(sourceDownloaders(prj.Repositories))
	if err != nil {
		chainDownloader = nil
	}
	return &stackMaterializer{
		workRoot:       prj.RootDir,
		cacheRoot:      ResolverCacheRoot(),
		repositories:   append([]project.Repository(nil), prj.Repositories...),
		store:          store,
		downloader:     chainDownloader,
		repositoryRoot: MaterializedRepositoryRoot(prj.RootDir),
		androidAARRoot: MaterializedAARRoot(prj.RootDir),
	}
}

func (m *stackMaterializer) Materialize(ctx context.Context, resolved *m2local.Resolved) (*m2local.Resolved, error) {
	if resolved == nil || m == nil || m.store == nil || m.downloader == nil {
		return resolved, nil
	}
	lockfilePins, err := m.lockfilePins(resolved)
	if err != nil || len(lockfilePins) == 0 {
		return resolved, err
	}
	publisher := mavenpublish.New(m.repositoryRoot)
	pinsByCoordinate := map[string]lockfile.Pin{}
	for _, pin := range lockfilePins {
		if err := m.downloader.Fetch(ctx, pin, m.store); err != nil {
			return nil, err
		}
		if err := publisher.PublishPin(ctx, pin, m.store); err != nil {
			return nil, err
		}
		pinsByCoordinate[pin.Coordinate.String()] = pin
	}
	projected := *resolved
	projected.CompileJars = m.rewriteJarPaths(resolved.CompileJars)
	projected.RuntimeJars = m.rewriteJarPaths(resolved.RuntimeJars)
	projected.TestJars = m.rewriteJarPaths(resolved.TestJars)
	projected.AndroidLibraries = m.materializeAndroidLibraries(ctx, resolved.AndroidLibraries, pinsByCoordinate)
	projected.CompileJars = m.rewriteAndroidClasspaths(projected.CompileJars, projected.AndroidLibraries)
	projected.RuntimeJars = m.rewriteAndroidClasspaths(projected.RuntimeJars, projected.AndroidLibraries)
	projected.TestJars = m.rewriteAndroidClasspaths(projected.TestJars, projected.AndroidLibraries)
	return &projected, nil
}

func (m *stackMaterializer) lockfilePins(resolved *m2local.Resolved) ([]lockfile.Pin, error) {
	// Fast path: if a persisted lockfile exists and the resolved output
	// has not changed since it was written, reuse it directly. This
	// avoids re-hashing every file on disk via produce.Produce.
	if cached, ok := loadCachedLockfile(m.workRoot, resolved); ok {
		return cached.Pins, nil
	}

	inputs, err := m2localbridge.FromResolved(resolved, gradlecache.ID)
	if err != nil || len(inputs) == 0 {
		inputs, err = m.inputsFromReplay(resolved)
		if err != nil {
			return nil, err
		}
	}
	if len(inputs) == 0 {
		return nil, nil
	}
	lf, err := produce.Produce(inputs, produce.Options{
		GeneratedAt: time.Now().UTC(),
		GritVersion: "dependencywiring",
	})
	if err != nil {
		return nil, err
	}

	// Persist the lockfile so subsequent builds with the same resolved
	// output can skip the produce step entirely.
	_ = saveLockfile(m.workRoot, lf, resolved)

	return lf.Pins, nil
}

func (m *stackMaterializer) inputsFromReplay(resolved *m2local.Resolved) ([]produce.Input, error) {
	if resolved == nil {
		return nil, nil
	}
	type pinHint struct {
		RepositoryURL string
		Capabilities  []string
		Binding       string
	}
	hints := map[lockfile.Coordinate]pinHint{}
	for _, pin := range resolved.Replay.Pins {
		coord, ok := parseResolutionCoordinate(pin.Coordinate)
		if !ok {
			continue
		}
		hint := hints[coord]
		if hint.RepositoryURL == "" {
			hint.RepositoryURL = pin.RepositoryURL
		}
		if hint.Binding == "" {
			hint.Binding = pin.Binding
		}
		hint.Capabilities = append(hint.Capabilities, pin.Capabilities...)
		hints[coord] = hint
	}
	for _, lib := range resolved.AndroidLibraries {
		coord, ok := coordinateFromAndroidLibraryID(lib.ID)
		if !ok {
			continue
		}
		hint := hints[coord]
		if hint.Binding == "" {
			hint.Binding = "android-library"
		}
		hints[coord] = hint
	}
	for _, path := range append(append([]string{}, resolved.CompileJars...), append(resolved.RuntimeJars, resolved.TestJars...)...) {
		coord, _, ok := coordinateAndNameFromGradlePath(path)
		if !ok {
			continue
		}
		if _, ok := hints[coord]; !ok {
			hints[coord] = pinHint{}
		}
	}
	coords := make([]lockfile.Coordinate, 0, len(hints))
	for coord := range hints {
		coords = append(coords, coord)
	}
	slices.SortFunc(coords, func(a, b lockfile.Coordinate) int {
		switch {
		case a.Group != b.Group:
			return strings.Compare(a.Group, b.Group)
		case a.Artifact != b.Artifact:
			return strings.Compare(a.Artifact, b.Artifact)
		default:
			return strings.Compare(a.Version, b.Version)
		}
	})
	inputs := make([]produce.Input, 0, len(coords))
	for _, coord := range coords {
		hint := hints[coord]
		files, err := moduleFilesForCoordinate(m.cacheRoot, coord, hint.Binding)
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			continue
		}
		inputs = append(inputs, produce.Input{
			Coordinate:   coord,
			RepositoryID: repositoryIDForURL(hint.RepositoryURL, m.repositories),
			Files:        files,
			Capabilities: uniqueSortedStrings(hint.Capabilities),
		})
	}
	return inputs, nil
}

func (m *stackMaterializer) rewriteJarPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		coord, name, ok := coordinateAndNameFromGradlePath(path)
		if !ok {
			out = append(out, path)
			continue
		}
		out = append(out, filepath.Join(m.repositoryRoot, mavenread.GroupPath(coord.Group), coord.Artifact, coord.Version, name))
	}
	return out
}

func (m *stackMaterializer) rewriteAndroidClasspaths(paths []string, libs []m2local.AndroidLibrary) []string {
	replacements := map[string]string{}
	for _, lib := range libs {
		coord, ok := coordinateFromAndroidLibraryID(lib.ID)
		if !ok || lib.ClassesJar == "" {
			continue
		}
		oldRoot := filepath.Join(sharedAARCompatibilityRoot(), coord.Group, coord.Artifact, coord.Version, "classes.jar")
		replacements[oldRoot] = lib.ClassesJar
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if replacement, ok := replacements[path]; ok {
			out = append(out, replacement)
			continue
		}
		out = append(out, path)
	}
	return out
}

func (m *stackMaterializer) materializeAndroidLibraries(ctx context.Context, libs []m2local.AndroidLibrary, pins map[string]lockfile.Pin) []m2local.AndroidLibrary {
	out := make([]m2local.AndroidLibrary, 0, len(libs))
	seen := map[string]bool{}
	for _, lib := range libs {
		if lib.ID == "" || seen[lib.ID] {
			continue
		}
		seen[lib.ID] = true
		coord, ok := coordinateFromAndroidLibraryID(lib.ID)
		if !ok {
			out = append(out, lib)
			continue
		}
		pin, ok := pins[coord.String()]
		if !ok {
			out = append(out, lib)
			continue
		}
		projected, err := m.materializeAARProjection(ctx, coord, pin)
		if err != nil {
			out = append(out, lib)
			continue
		}
		out = append(out, projected)
	}
	return out
}

func (m *stackMaterializer) materializeAARProjection(ctx context.Context, coord lockfile.Coordinate, pin lockfile.Pin) (m2local.AndroidLibrary, error) {
	outDir := filepath.Join(m.androidAARRoot, coord.Group, coord.Artifact, coord.Version)
	readyPath := filepath.Join(outDir, ".ready")
	if _, err := os.Stat(readyPath); err == nil {
		return materializedAndroidLibrary(coord, outDir), nil
	}
	var primaryHash cas.Hash
	foundPrimary := false
	for _, file := range pin.Files {
		if file.Kind == lockfile.FileKindPrimary && strings.HasSuffix(strings.ToLower(file.Name), ".aar") {
			primaryHash = file.Hash
			foundPrimary = true
			break
		}
	}
	if !foundPrimary {
		return m2local.AndroidLibrary{}, os.ErrNotExist
	}
	result, err := aarextract.Extract(ctx, m.store, primaryHash)
	if err != nil {
		return m2local.AndroidLibrary{}, err
	}
	tmpDir := outDir + ".tmp"
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return m2local.AndroidLibrary{}, err
	}
	if output, ok := result.Output(aarextract.RoleClassesJar); ok {
		if err := copyBlobToFile(ctx, m.store, output.Blob.Hash, filepath.Join(tmpDir, "classes.jar")); err != nil {
			return m2local.AndroidLibrary{}, err
		}
	}
	if output, ok := result.Output(aarextract.RoleAndroidManifest); ok {
		if err := copyBlobToFile(ctx, m.store, output.Blob.Hash, filepath.Join(tmpDir, "AndroidManifest.xml")); err != nil {
			return m2local.AndroidLibrary{}, err
		}
	}
	if output, ok := result.Output(aarextract.RoleResourceTree); ok {
		if err := expandZipBlobToDir(ctx, m.store, output.Blob.Hash, filepath.Join(tmpDir, "res")); err != nil {
			return m2local.AndroidLibrary{}, err
		}
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".ready"), []byte("ok\n"), 0o644); err != nil {
		return m2local.AndroidLibrary{}, err
	}
	_ = os.RemoveAll(outDir)
	if err := os.Rename(tmpDir, outDir); err != nil {
		return m2local.AndroidLibrary{}, err
	}
	return materializedAndroidLibrary(coord, outDir), nil
}

func materializedAndroidLibrary(coord lockfile.Coordinate, outDir string) m2local.AndroidLibrary {
	return m2local.AndroidLibrary{
		ID:           "maven:" + coord.Group + ":" + coord.Artifact + ":" + coord.Version,
		ClassesJar:   existingFile(filepath.Join(outDir, "classes.jar")),
		ManifestPath: existingFile(filepath.Join(outDir, "AndroidManifest.xml")),
		ResDir:       existingDir(filepath.Join(outDir, "res")),
	}
}

func sourceDownloaders(repos []project.Repository) []downloader.Downloader {
	var sources []downloader.Downloader
	if root := ResolverCacheRoot(); root != "" {
		sources = append(sources, gradlecache.New(root))
	}
	if root := mavenread.DefaultRoot(); root != "" {
		sources = append(sources, mavenread.New(root))
	}
	for _, repo := range repos {
		if dl := downloaderForRepository(repo); dl != nil {
			sources = append(sources, dl)
		}
	}
	return sources
}

func downloaderForRepository(repo project.Repository) downloader.Downloader {
	switch repo.Kind {
	case "mavenLocal":
		if root := fileURLPath(repo.URL); root != "" {
			return mavenread.New(root)
		}
		if root := mavenread.DefaultRoot(); root != "" {
			return mavenread.New(root)
		}
		return nil
	case "maven", "google", "mavenCentral", "gradlePluginPortal", "jcenter":
		if root := fileURLPath(repo.URL); root != "" {
			return mavenread.New(root)
		}
		if strings.TrimSpace(repo.URL) == "" {
			return nil
		}
		remote, err := mavenremote.New(repo.URL, mavenremote.WithID(firstNonEmpty(repo.Name, repo.Kind)))
		if err != nil {
			return nil
		}
		wrapped, err := retry.New(remote, retry.WithAttempts(3), retry.WithBackoff(func(attempt int) time.Duration {
			return time.Duration(attempt) * 10 * time.Millisecond
		}))
		if err != nil {
			return remote
		}
		return wrapped
	default:
		return nil
	}
}

func moduleFilesForCoordinate(cacheRoot string, coord lockfile.Coordinate, binding string) ([]produce.FileInput, error) {
	base := filepath.Join(cacheRoot, coord.Group, coord.Artifact, coord.Version)
	hashDirs, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	candidates := map[string]string{}
	hashNames := make([]string, 0, len(hashDirs))
	for _, entry := range hashDirs {
		if entry.IsDir() {
			hashNames = append(hashNames, entry.Name())
		}
	}
	slices.Sort(hashNames)
	for _, hashDir := range hashNames {
		files, err := os.ReadDir(filepath.Join(base, hashDir))
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			if _, ok := candidates[file.Name()]; ok {
				continue
			}
			candidates[file.Name()] = filepath.Join(base, hashDir, file.Name())
		}
	}
	var primary produce.FileInput
	hasPrimary := false
	switch binding {
	case "android-library":
		primary, hasPrimary = firstMatchingFile(candidates, func(name string) bool {
			return strings.HasSuffix(strings.ToLower(name), ".aar")
		})
	default:
		primary, hasPrimary = firstMatchingFile(candidates, func(name string) bool {
			lower := strings.ToLower(name)
			return strings.HasSuffix(lower, ".jar") && !strings.HasSuffix(lower, "-sources.jar") && !strings.HasSuffix(lower, "-javadoc.jar")
		})
		if !hasPrimary {
			primary, hasPrimary = firstMatchingFile(candidates, func(name string) bool {
				return strings.HasSuffix(strings.ToLower(name), ".aar")
			})
		}
	}
	var out []produce.FileInput
	if hasPrimary {
		primary.Kind = lockfile.FileKindPrimary
		out = append(out, primary)
	}
	appendKind := func(kind lockfile.FileKind, match func(string) bool) {
		if file, ok := firstMatchingFile(candidates, match); ok {
			file.Kind = kind
			out = append(out, file)
		}
	}
	appendKind(lockfile.FileKindPOM, func(name string) bool { return strings.HasSuffix(strings.ToLower(name), ".pom") })
	appendKind(lockfile.FileKindModule, func(name string) bool { return strings.HasSuffix(strings.ToLower(name), ".module") })
	appendKind(lockfile.FileKindSources, func(name string) bool { return strings.HasSuffix(strings.ToLower(name), "-sources.jar") })
	appendKind(lockfile.FileKindJavadoc, func(name string) bool { return strings.HasSuffix(strings.ToLower(name), "-javadoc.jar") })
	return out, nil
}

func firstMatchingFile(candidates map[string]string, match func(string) bool) (produce.FileInput, bool) {
	names := make([]string, 0, len(candidates))
	for name := range candidates {
		if match(name) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return produce.FileInput{}, false
	}
	slices.Sort(names)
	name := names[0]
	return produce.FileInput{Name: name, Path: candidates[name]}, true
}

func repositoryIDForURL(rawURL string, repos []project.Repository) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return gradlecache.ID
	}
	for _, repo := range repos {
		if sameRepositoryURL(repo.URL, rawURL) {
			return firstNonEmpty(repo.Name, repo.Kind, rawURL)
		}
	}
	return rawURL
}

func sameRepositoryURL(a, b string) bool {
	return strings.TrimRight(strings.TrimSpace(a), "/") == strings.TrimRight(strings.TrimSpace(b), "/")
}

func fileURLPath(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return filepath.FromSlash(u.Path)
}

func parseResolutionCoordinate(value string) (lockfile.Coordinate, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) < 3 {
		return lockfile.Coordinate{}, false
	}
	coord := lockfile.Coordinate{
		Group:    parts[0],
		Artifact: parts[1],
		Version:  parts[2],
	}
	if len(parts) > 3 {
		coord.Classifier = parts[3]
	}
	return coord, coord.Group != "" && coord.Artifact != "" && coord.Version != ""
}

func coordinateFromAndroidLibraryID(id string) (lockfile.Coordinate, bool) {
	parts := strings.Split(strings.TrimSpace(id), ":")
	if len(parts) != 4 || parts[0] != "maven" {
		return lockfile.Coordinate{}, false
	}
	return lockfile.Coordinate{
		Group:    parts[1],
		Artifact: parts[2],
		Version:  parts[3],
	}, true
}

func coordinateAndNameFromGradlePath(path string) (lockfile.Coordinate, string, bool) {
	marker := filepath.Join("files-2.1") + string(os.PathSeparator)
	idx := strings.Index(path, marker)
	if idx < 0 {
		return lockfile.Coordinate{}, "", false
	}
	rest := strings.Split(path[idx+len(marker):], string(os.PathSeparator))
	if len(rest) < 5 {
		return lockfile.Coordinate{}, "", false
	}
	return lockfile.Coordinate{
		Group:    rest[0],
		Artifact: rest[1],
		Version:  rest[2],
	}, rest[4], true
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sharedAARCompatibilityRoot() string {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".grit", "aar")
}

func existingFile(path string) string {
	info, err := os.Stat(path)
	if err == nil && info.Mode().IsRegular() {
		return path
	}
	return ""
}

func existingDir(path string) string {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return path
	}
	return ""
}
