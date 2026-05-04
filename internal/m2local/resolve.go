package m2local

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/kaeawc/grit/internal/catalog"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

const resolvedCacheVersion = "16"

type resolvedCacheKeyData struct {
	CacheVersion string                    `json:"cacheVersion"`
	Topology     CacheTopology             `json:"topology"`
	Repositories []project.Repository      `json:"repositories"`
	Catalog      *catalog.Catalog          `json:"catalog"`
	Deps         *modulebuild.Dependencies `json:"deps"`
}

func (r *Resolver) Resolve(deps *modulebuild.Dependencies) (*Resolved, error) {
	r.resetReport()
	r.resetReplay()
	if cached, err := r.trackResolved("loadResolvedCache", func() (*Resolved, error) {
		return r.loadResolvedCache(deps)
	}); err == nil && cached != nil {
		return cached, nil
	}
	platforms := r.seedPlatforms()
	mainRoots, err := r.trackResolveCoords("expandMainRefs", func() ([]Coordinate, error) {
		return r.expandRefs(append(append([]modulebuild.Ref{}, deps.Main...), deps.Debug...), platforms)
	})
	if err != nil {
		return nil, err
	}
	testPlatforms := clonePlatforms(platforms)
	testRoots, err := r.trackResolveCoords("expandTestRefs", func() ([]Coordinate, error) {
		return r.expandRefs(deps.Test, testPlatforms)
	})
	if err != nil {
		return nil, err
	}
	runtimeRoots, err := r.trackResolveCoords("expandRuntimeRefs", func() ([]Coordinate, error) {
		return r.expandRefs(deps.RuntimeOnly, clonePlatforms(platforms))
	})
	if err != nil {
		return nil, err
	}
	testRuntimeRoots, err := r.trackResolveCoords("expandTestRuntimeRefs", func() ([]Coordinate, error) {
		return r.expandRefs(deps.TestRuntimeOnly, clonePlatforms(testPlatforms))
	})
	if err != nil {
		return nil, err
	}
	desugarRoots, err := r.trackResolveCoords("expandDesugarRefs", func() ([]Coordinate, error) {
		return r.expandRefs(deps.CoreLibraryDesugaring, clonePlatforms(platforms))
	})
	if err != nil {
		return nil, err
	}
	type closureResult struct {
		jars []string
		libs []AndroidLibrary
		err  error
	}
	results := make([]closureResult, 3)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		results[0].jars, results[0].libs, results[0].err = r.trackResolveClosure("resolveMainClosure", mainRoots)
	}()
	go func() {
		defer wg.Done()
		results[1].jars, results[1].libs, results[1].err = r.trackResolveClosure("resolveTestClosure", testRoots)
	}()
	go func() {
		defer wg.Done()
		results[2].jars, results[2].libs, results[2].err = r.trackResolveClosure("resolveDesugarClosure", desugarRoots)
	}()
	wg.Wait()
	for _, result := range results {
		if result.err != nil {
			return nil, result.err
		}
	}
	mainSet, mainAndroid := results[0].jars, results[0].libs
	testSet := results[1].jars
	desugarSet := results[2].jars
	runtimeSet, runtimeAndroid, err := r.trackResolveClosure("resolveRuntimeClosure", runtimeRoots)
	if err != nil {
		return nil, err
	}
	testRuntimeSet, testRuntimeAndroid, err := r.trackResolveClosure("resolveTestRuntimeClosure", testRuntimeRoots)
	if err != nil {
		return nil, err
	}

	resolved := &Resolved{
		CompileJars:      mainSet,
		RuntimeJars:      mergeUnique(mainSet, runtimeSet, desugarSet),
		TestJars:         mergeUnique(mainSet, runtimeSet, testSet, testRuntimeSet),
		AndroidLibraries: uniqueAndroidLibraries(append(append(append([]AndroidLibrary{}, mainAndroid...), runtimeAndroid...), testRuntimeAndroid...)),
		Report:           r.snapshotReport(),
		Replay:           r.snapshotReplay(),
	}
	resolved.Lockfile = deriveResolutionLockfile(resolved.Report, resolved.Replay)
	if err := r.trackResolveErr("saveResolvedCache", func() error {
		return r.saveResolvedCache(deps, resolved)
	}); err != nil {
		return resolved, nil
	}
	return resolved, nil
}

func (r *Resolver) resetReport() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report = ResolutionReport{}
}

func (r *Resolver) resetReplay() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replay = ResolutionReplay{}
}

func (r *Resolver) snapshotReport() ResolutionReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	return ResolutionReport{
		Selections: append([]ResolutionSelection(nil), r.report.Selections...),
		Conflicts:  append([]ResolutionConflict(nil), r.report.Conflicts...),
	}
}

func (r *Resolver) snapshotReplay() ResolutionReplay {
	r.mu.Lock()
	defer r.mu.Unlock()
	pins := append([]ResolutionPin(nil), r.replay.Pins...)
	sort.Slice(pins, func(i, j int) bool {
		if pins[i].Coordinate != pins[j].Coordinate {
			return pins[i].Coordinate < pins[j].Coordinate
		}
		if pins[i].Variant != pins[j].Variant {
			return pins[i].Variant < pins[j].Variant
		}
		if pins[i].RepositoryURL != pins[j].RepositoryURL {
			return pins[i].RepositoryURL < pins[j].RepositoryURL
		}
		return strings.Join(pins[i].Capabilities, ",") < strings.Join(pins[j].Capabilities, ",")
	})
	return ResolutionReplay{Pins: pins}
}

func (r *Resolver) addSelection(selection ResolutionSelection) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.report.Selections {
		if existing.Kind == selection.Kind &&
			existing.Coordinate == selection.Coordinate &&
			existing.Chosen == selection.Chosen &&
			existing.Reason == selection.Reason &&
			metadataSourceKey(existing.MetadataSource) == metadataSourceKey(selection.MetadataSource) {
			return
		}
	}
	r.report.Selections = append(r.report.Selections, selection)
}

func (r *Resolver) addConflict(conflict ResolutionConflict) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.report.Conflicts {
		if existing.Kind == conflict.Kind && existing.Module == conflict.Module && existing.Selected == conflict.Selected && existing.Discarded == conflict.Discarded {
			return
		}
	}
	r.report.Conflicts = append(r.report.Conflicts, conflict)
}

func (r *Resolver) addReplayPin(pin ResolutionPin) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.replay.Pins {
		if existing.Coordinate == pin.Coordinate &&
			existing.Variant == pin.Variant &&
			existing.RepositoryURL == pin.RepositoryURL &&
			strings.Join(existing.Capabilities, ",") == strings.Join(pin.Capabilities, ",") {
			return
		}
	}
	r.replay.Pins = append(r.replay.Pins, pin)
}

func (r *Resolver) trackResolveCoords(name string, fn func() ([]Coordinate, error)) ([]Coordinate, error) {
	if r.Tracker == nil || !r.Tracker.IsEnabled() {
		return fn()
	}
	var out []Coordinate
	var err error
	trackErr := r.Tracker.Track(name, func() error {
		out, err = fn()
		return err
	})
	if trackErr != nil {
		return nil, trackErr
	}
	return out, err
}

func (r *Resolver) trackResolved(name string, fn func() (*Resolved, error)) (*Resolved, error) {
	if r.Tracker == nil || !r.Tracker.IsEnabled() {
		return fn()
	}
	var out *Resolved
	var err error
	trackErr := r.Tracker.Track(name, func() error {
		out, err = fn()
		return err
	})
	if trackErr != nil {
		return nil, trackErr
	}
	return out, err
}

func (r *Resolver) trackResolveClosure(name string, roots []Coordinate) ([]string, []AndroidLibrary, error) {
	if r.Tracker == nil || !r.Tracker.IsEnabled() {
		return r.resolveClosure(roots)
	}
	var jars []string
	var libs []AndroidLibrary
	var err error
	trackErr := r.Tracker.Track(name, func() error {
		jars, libs, err = r.resolveClosure(roots)
		return err
	})
	if trackErr != nil {
		return nil, nil, trackErr
	}
	return jars, libs, err
}

func (r *Resolver) trackResolveErr(name string, fn func() error) error {
	if r.Tracker == nil || !r.Tracker.IsEnabled() {
		return fn()
	}
	return r.Tracker.Track(name, fn)
}

func (r *Resolver) loadResolvedCache(deps *modulebuild.Dependencies) (*Resolved, error) {
	path, err := r.resolvedCachePath(deps)
	if err != nil {
		return nil, err
	}
	if !fileExists(path) {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var envelope ResolvedEnvelope
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.SchemaVersion == 1 && envelope.Format == "m2local-resolved" {
		resolved := envelope.Resolved
		resolved.Lockfile = ensureResolutionLockfile(resolved)
		if err := r.ensureResolvedMaterialized(&resolved); err != nil {
			return nil, nil
		}
		return &resolved, nil
	}
	var resolved Resolved
	if err := json.Unmarshal(data, &resolved); err != nil {
		return nil, err
	}
	resolved.Lockfile = ensureResolutionLockfile(resolved)
	if err := r.ensureResolvedMaterialized(&resolved); err != nil {
		return nil, nil
	}
	return &resolved, nil
}

func (r *Resolver) saveResolvedCache(deps *modulebuild.Dependencies, resolved *Resolved) error {
	path, err := r.resolvedCachePath(deps)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	resolved.Lockfile = deriveResolutionLockfile(resolved.Report, resolved.Replay)
	data, err := json.Marshal(ResolvedEnvelope{
		SchemaVersion: 1,
		Format:        "m2local-resolved",
		Topology:      r.Topology(),
		Resolved:      *resolved,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	if err := writeResolutionReportArtifact(resolvedReportPath(path), resolved.Report); err != nil {
		return err
	}
	if err := writeResolutionReplayArtifact(resolvedReplayPath(path), resolved.Replay); err != nil {
		return err
	}
	return writeResolutionLockfileArtifact(resolvedLockfilePath(path), resolved.Lockfile)
}

func loadOrDeriveResolutionLockfile(lockfilePath string, resolved Resolved) ResolutionLockfile {
	if lockfilePath != "" && fileExists(lockfilePath) {
		if lockfile, err := readResolutionLockfileArtifact(lockfilePath); err == nil {
			return normalizedResolutionLockfile(lockfile)
		}
	}
	return ensureResolutionLockfile(resolved)
}

func writeResolutionLockfileArtifact(path string, lockfile ResolutionLockfile) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(lockfile)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeResolutionReportArtifact(path string, report ResolutionReport) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(ResolutionReportArtifact{
		SchemaVersion: 1,
		Format:        "m2local-report",
		Report:        report,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeResolutionReplayArtifact(path string, replay ResolutionReplay) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(ResolutionReplayArtifact{
		SchemaVersion: 1,
		Format:        "m2local-replay",
		Replay:        replay,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readResolutionLockfileArtifact(path string) (ResolutionLockfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ResolutionLockfile{}, err
	}
	var lockfile ResolutionLockfile
	if err := json.Unmarshal(data, &lockfile); err != nil {
		return ResolutionLockfile{}, err
	}
	return lockfile, nil
}

func ensureResolutionLockfile(resolved Resolved) ResolutionLockfile {
	if resolved.Lockfile.SchemaVersion != 0 || resolved.Lockfile.Format != "" || len(resolved.Lockfile.Pins) != 0 || len(resolved.Lockfile.Selections) != 0 || len(resolved.Lockfile.Conflicts) != 0 {
		return normalizedResolutionLockfile(resolved.Lockfile)
	}
	return deriveResolutionLockfile(resolved.Report, resolved.Replay)
}

func deriveResolutionLockfile(report ResolutionReport, replay ResolutionReplay) ResolutionLockfile {
	lockfile := ResolutionLockfile{
		SchemaVersion: 1,
		Format:        "m2local-lockfile",
		Pins:          append([]ResolutionPin(nil), replay.Pins...),
		Selections:    append([]ResolutionSelection(nil), report.Selections...),
		Conflicts:     append([]ResolutionConflict(nil), report.Conflicts...),
	}
	return normalizedResolutionLockfile(lockfile)
}

func normalizedResolutionLockfile(lockfile ResolutionLockfile) ResolutionLockfile {
	pins := append([]ResolutionPin(nil), lockfile.Pins...)
	for i := range pins {
		pins[i].Capabilities = append([]string(nil), pins[i].Capabilities...)
		sort.Strings(pins[i].Capabilities)
	}
	sort.Slice(pins, func(i, j int) bool {
		if pins[i].Coordinate != pins[j].Coordinate {
			return pins[i].Coordinate < pins[j].Coordinate
		}
		if pins[i].Variant != pins[j].Variant {
			return pins[i].Variant < pins[j].Variant
		}
		return strings.Join(pins[i].Capabilities, ",") < strings.Join(pins[j].Capabilities, ",")
	})

	selections := append([]ResolutionSelection(nil), lockfile.Selections...)
	for i := range selections {
		selections[i].Alternates = append([]string(nil), selections[i].Alternates...)
		selections[i].Capabilities = append([]string(nil), selections[i].Capabilities...)
		sort.Strings(selections[i].Alternates)
		sort.Strings(selections[i].Capabilities)
	}
	sort.Slice(selections, func(i, j int) bool {
		if selections[i].Kind != selections[j].Kind {
			return selections[i].Kind < selections[j].Kind
		}
		if selections[i].Coordinate != selections[j].Coordinate {
			return selections[i].Coordinate < selections[j].Coordinate
		}
		if selections[i].Chosen != selections[j].Chosen {
			return selections[i].Chosen < selections[j].Chosen
		}
		if selections[i].Reason != selections[j].Reason {
			return selections[i].Reason < selections[j].Reason
		}
		return metadataSourceKey(selections[i].MetadataSource) < metadataSourceKey(selections[j].MetadataSource)
	})

	conflicts := append([]ResolutionConflict(nil), lockfile.Conflicts...)
	for i := range conflicts {
		conflicts[i].Coordinates = append([]string(nil), conflicts[i].Coordinates...)
		sort.Strings(conflicts[i].Coordinates)
	}
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Kind != conflicts[j].Kind {
			return conflicts[i].Kind < conflicts[j].Kind
		}
		if conflicts[i].Module != conflicts[j].Module {
			return conflicts[i].Module < conflicts[j].Module
		}
		if conflicts[i].Selected != conflicts[j].Selected {
			return conflicts[i].Selected < conflicts[j].Selected
		}
		return conflicts[i].Discarded < conflicts[j].Discarded
	})

	return ResolutionLockfile{
		SchemaVersion: max(lockfile.SchemaVersion, 1),
		Format:        firstNonEmpty(lockfile.Format, "m2local-lockfile"),
		Pins:          pins,
		Selections:    selections,
		Conflicts:     conflicts,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func metadataSourceKey(source *ResolutionMetadataSource) string {
	if source == nil {
		return ""
	}
	return strings.Join([]string{
		source.Kind,
		source.Path,
		source.RepositoryURL,
		boolString(source.Fetched),
	}, "|")
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (r *Resolver) resolvedCacheKeyData(deps *modulebuild.Dependencies) resolvedCacheKeyData {
	return resolvedCacheKeyData{
		CacheVersion: resolvedCacheVersion,
		Topology:     r.Topology(),
		Repositories: r.Repositories,
		Catalog:      r.Catalog,
		Deps:         deps,
	}
}

func (r *Resolver) resolvedCacheKey(deps *modulebuild.Dependencies) (string, error) {
	keyData, err := json.Marshal(r.resolvedCacheKeyData(deps))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(keyData)
	return hex.EncodeToString(sum[:]), nil
}

func (r *Resolver) resolvedCachePath(deps *modulebuild.Dependencies) (string, error) {
	key, err := r.resolvedCacheKey(deps)
	if err != nil {
		return "", err
	}
	return filepath.Join(sharedResolveCacheRoot(), key+".json"), nil
}
