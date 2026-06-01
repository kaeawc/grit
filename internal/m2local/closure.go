package m2local

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

func (r *Resolver) resolveClosure(roots []Coordinate) ([]string, []AndroidLibrary, error) {
	seen := map[string]bool{}
	selectedByModule := map[string]string{}
	// rootPinned remembers the exact version each root coord declared.
	// Roots are the project's direct catalog / build-script declarations;
	// Gradle's default conflict policy treats them as strict-version
	// requests that transitive deps may not bump higher. Without the pin
	// grit would pick the highest transitive version, which regularly
	// lands on a newer Kotlin-metadata-version jar that the project's
	// pinned Kotlin compiler can't read (e.g. coroutines 1.11.0 metadata
	// 2.2.0 on a project on Kotlin 2.0.20).
	rootPinned := map[string]string{}
	for _, root := range roots {
		rootPinned[root.Group+":"+root.Module] = root.Version
	}
	// Catalog-declared library pins act as project-global root pins, even
	// for modules where the library appears only as a transitive dep.
	// This mirrors Gradle's behavior: a versions-catalog pin is the
	// project's strict version, and transitives can't bump above it. Without
	// this, a module that only sees kotlinx-coroutines-core transitively
	// (e.g. via androidx.lifecycle) would resolve to whatever higher
	// version that transitive declared, picking a Kotlin-metadata
	// version newer than the project's pinned Kotlin compiler can read.
	//
	// We also pin the conventional KMP variant module keys (-jvm, -android)
	// derived from each umbrella, since the resolver sometimes encounters
	// those directly as transitive coords. The Catalog only knows about
	// the umbrella (kotlinx-coroutines-core) but the closure may see both
	// the umbrella and the platform variant (kotlinx-coroutines-core-jvm)
	// as separate module keys, and we want both to pin to the same
	// catalog-declared version.
	if r != nil && r.Catalog != nil {
		pin := func(group, module, version string) {
			if group == "" || module == "" || version == "" {
				return
			}
			key := group + ":" + module
			if _, already := rootPinned[key]; already {
				return
			}
			rootPinned[key] = version
		}
		for _, lib := range r.Catalog.Libraries {
			pin(lib.Group, lib.Name, lib.Version)
			pin(lib.Group, lib.Name+"-jvm", lib.Version)
			pin(lib.Group, lib.Name+"-android", lib.Version)
		}
	}
	type artifactEntry struct {
		coord          Coordinate
		artifact       string
		androidLibrary *AndroidLibrary
	}
	var entries []artifactEntry
	queue := append([]Coordinate{}, roots...)

	recordConflict := func(moduleKey, selected, discarded, reason string) {
		r.addConflict(ResolutionConflict{
			Kind:        "version_conflict",
			Module:      moduleKey,
			Selected:    selected,
			Discarded:   discarded,
			Reason:      reason,
			Coordinates: []string{moduleKey + ":" + selected, moduleKey + ":" + discarded},
		})
	}
	// updateSelected may rewrite coord's Version to the catalog-pinned
	// version before deciding what to select. The (possibly rewritten)
	// coord is returned; callers must use it for the seen-set and queue
	// so the pinned version is what actually gets resolved.
	updateSelected := func(coord Coordinate) Coordinate {
		moduleKey := coord.Group + ":" + coord.Module
		if pinned, hasPin := rootPinned[moduleKey]; hasPin && coord.Version != pinned {
			recordConflict(moduleKey, pinned, coord.Version, "root_pin")
			coord.Version = pinned
		}
		current, ok := selectedByModule[moduleKey]
		if !ok {
			selectedByModule[moduleKey] = coord.Version
			return coord
		}
		if current == coord.Version {
			return coord
		}
		if compareVersionStrings(coord.Version, current) > 0 {
			selectedByModule[moduleKey] = coord.Version
			recordConflict(moduleKey, coord.Version, current, "highest_version")
			return coord
		}
		recordConflict(moduleKey, current, coord.Version, "highest_version")
		return Coordinate{Group: coord.Group, Module: coord.Module, Version: current}
	}

	for len(queue) > 0 {
		frontier := append([]Coordinate{}, queue...)
		queue = nil
		var pending []Coordinate
		for _, coord := range frontier {
			coord = updateSelected(coord)
			key := coord.Group + ":" + coord.Module + ":" + coord.Version
			if seen[key] {
				continue
			}
			seen[key] = true
			pending = append(pending, coord)
		}
		if len(pending) == 0 {
			continue
		}
		results := make([]resolveResult, len(pending))
		workers := len(pending)
		if workers > 8 {
			workers = 8
		}
		var wg sync.WaitGroup
		indexCh := make(chan int)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for idx := range indexCh {
					artifact, androidLibrary, deps, err := r.resolveOne(pending[idx])
					results[idx] = resolveResult{
						artifact:       artifact,
						androidLibrary: androidLibrary,
						deps:           deps,
						err:            err,
					}
				}
			}()
		}
		for i := range pending {
			indexCh <- i
		}
		close(indexCh)
		wg.Wait()
		for idx, result := range results {
			if result.err != nil {
				return nil, nil, result.err
			}
			entries = append(entries, artifactEntry{
				coord:          pending[idx],
				artifact:       result.artifact,
				androidLibrary: result.androidLibrary,
			})
			queue = append(queue, result.deps...)
		}
	}

	var jars []string
	var androidLibraries []AndroidLibrary
	for _, entry := range entries {
		moduleKey := entry.coord.Group + ":" + entry.coord.Module
		if selectedByModule[moduleKey] != entry.coord.Version {
			continue
		}
		if entry.artifact != "" {
			jars = append(jars, entry.artifact)
		}
		if entry.androidLibrary != nil && (entry.androidLibrary.ManifestPath != "" || entry.androidLibrary.ResDir != "") {
			androidLibraries = append(androidLibraries, *entry.androidLibrary)
		}
	}

	return jars, uniqueAndroidLibraries(androidLibraries), nil
}

func applyExcludes(deps []Coordinate, excludes []Exclude) []Coordinate {
	if len(excludes) == 0 || len(deps) == 0 {
		return deps
	}
	var out []Coordinate
	for _, dep := range deps {
		if matchesAnyExclude(dep, excludes) {
			continue
		}
		out = append(out, dep)
	}
	return out
}

func matchesAnyExclude(coord Coordinate, excludes []Exclude) bool {
	for _, exclude := range excludes {
		if matchesExclude(coord, exclude) {
			return true
		}
	}
	return false
}

func matchesExclude(coord Coordinate, exclude Exclude) bool {
	groupMatches := exclude.Group == "*" || exclude.Group == "" || exclude.Group == coord.Group
	moduleMatches := exclude.Module == "*" || exclude.Module == "" || exclude.Module == coord.Module
	return groupMatches && moduleMatches
}

func (r *Resolver) resolveOne(coord Coordinate) (string, *AndroidLibrary, []Coordinate, error) {
	return r.resolveOneDepth(coord, 0)
}

func (r *Resolver) resolveOneDepth(coord Coordinate, depth int) (string, *AndroidLibrary, []Coordinate, error) {
	key := coordinateID(coord)
	r.mu.Lock()
	if call, ok := r.inflight[key]; ok {
		r.mu.Unlock()
		<-call.done
		return call.result.artifact, call.result.androidLibrary, call.result.deps, call.result.err
	}
	call := &resolveCall{done: make(chan struct{})}
	r.inflight[key] = call
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.inflight, key)
		close(call.done)
		r.mu.Unlock()
	}()
	if coord.Group == "org.jetbrains.kotlin" && strings.HasPrefix(coord.Module, "kotlin-stdlib") {
		call.result = resolveResult{}
		return "", nil, nil, nil
	}
	if coord.Group == "io.mockk" && coord.Module == "mockk" {
		alt := coord
		alt.Module = "mockk-jvm"
		if normalized, ok := r.normalizeFallbackCoordinate(alt); ok {
			alt = normalized
		}
		if _, err := os.Stat(r.moduleBasePath(alt)); err == nil {
			r.addSelection(ResolutionSelection{
				Kind:       "module_redirect",
				Coordinate: coord.Group + ":" + coord.Module + ":" + coord.Version,
				Chosen:     alt.Group + ":" + alt.Module + ":" + alt.Version,
				Reason:     "mockk_jvm_preference",
			})
			coord = alt
		}
	}
	modulePath := r.moduleBasePath(coord)
	moduleFile, _ := findFile(modulePath, ".module")
	if moduleFile == "" {
		if fetched, fetchErr := r.fetchModuleMetadata(coord); fetchErr == nil {
			moduleFile = fetched
		}
	}
	if moduleFile != "" {
		artifact, androidLibrary, deps, err := r.resolveModuleMetadata(coord, moduleFile, r.metadataSourceForPath(moduleFile, "module"), depth)
		if err != nil && errors.Is(err, errAvailableAtDepthExceeded) {
			call.result = resolveResult{err: err}
			return "", nil, nil, err
		}
		if err == nil && (artifact != "" || androidLibrary != nil) {
			deps = applyExcludes(deps, coord.Excludes)
			call.result = resolveResult{artifact: artifact, androidLibrary: androidLibrary, deps: deps}
			return artifact, androidLibrary, deps, nil
		}
	}

	pomFile, err := findFile(modulePath, ".pom")
	if err != nil {
		if fetched, fetchErr := r.fetchPOM(coord); fetchErr == nil {
			pomFile = fetched
		}
	}
	if moduleFile == "" && pomFile == "" {
		if cached := r.findCachedVersion(coord.Group, coord.Module); cached != "" && cached != coord.Version {
			r.addSelection(ResolutionSelection{
				Kind:       "version_fallback",
				Coordinate: coord.Group + ":" + coord.Module + ":" + coord.Version,
				Chosen:     coord.Group + ":" + coord.Module + ":" + cached,
				Reason:     "cached_local_version",
			})
			coord.Version = cached
			modulePath = r.moduleBasePath(coord)
			moduleFile, _ = findFile(modulePath, ".module")
			if moduleFile == "" {
				if fetched, fetchErr := r.fetchModuleMetadata(coord); fetchErr == nil {
					moduleFile = fetched
				}
			}
			if moduleFile != "" {
				artifact, androidLibrary, deps, err := r.resolveModuleMetadata(coord, moduleFile, r.metadataSourceForPath(moduleFile, "module"), depth)
				if err != nil && errors.Is(err, errAvailableAtDepthExceeded) {
					call.result = resolveResult{err: err}
					return "", nil, nil, err
				}
				if err == nil && (artifact != "" || androidLibrary != nil) {
					deps = applyExcludes(deps, coord.Excludes)
					call.result = resolveResult{artifact: artifact, androidLibrary: androidLibrary, deps: deps}
					return artifact, androidLibrary, deps, nil
				}
			}
			pomFile, _ = findFile(modulePath, ".pom")
			if pomFile == "" {
				if fetched, fetchErr := r.fetchPOM(coord); fetchErr == nil {
					pomFile = fetched
				}
			}
		}
	}
	if moduleFile == "" && pomFile == "" {
		if alt, ok := r.preferJVMSibling(coord); ok {
			r.addSelection(ResolutionSelection{
				Kind:       "module_redirect",
				Coordinate: coord.Group + ":" + coord.Module + ":" + coord.Version,
				Chosen:     alt.Group + ":" + alt.Module + ":" + alt.Version,
				Reason:     "prefer_jvm_sibling",
			})
			coord = alt
			modulePath = r.moduleBasePath(coord)
			moduleFile, _ = findFile(modulePath, ".module")
			if moduleFile == "" {
				if fetched, fetchErr := r.fetchModuleMetadata(coord); fetchErr == nil {
					moduleFile = fetched
				}
			}
			if moduleFile != "" {
				artifact, androidLibrary, deps, err := r.resolveModuleMetadata(coord, moduleFile, r.metadataSourceForPath(moduleFile, "module"), depth)
				if err != nil && errors.Is(err, errAvailableAtDepthExceeded) {
					call.result = resolveResult{err: err}
					return "", nil, nil, err
				}
				if err == nil && (artifact != "" || androidLibrary != nil) {
					deps = applyExcludes(deps, coord.Excludes)
					call.result = resolveResult{artifact: artifact, androidLibrary: androidLibrary, deps: deps}
					return artifact, androidLibrary, deps, nil
				}
			}
			pomFile, _ = findFile(modulePath, ".pom")
			if pomFile == "" {
				if fetched, fetchErr := r.fetchPOM(coord); fetchErr == nil {
					pomFile = fetched
				}
			}
		}
	}
	if moduleFile == "" && pomFile == "" && !strings.HasSuffix(coord.Module, "-jvm") && !strings.HasSuffix(coord.Module, "-android") {
		alt := coord
		alt.Module = coord.Module + "-android"
		if normalized, ok := r.normalizeFallbackCoordinate(alt); ok {
			alt = normalized
		}
		if _, altErr := os.Stat(r.moduleBasePath(alt)); altErr == nil {
			r.addSelection(ResolutionSelection{
				Kind:       "module_redirect",
				Coordinate: coord.Group + ":" + coord.Module + ":" + coord.Version,
				Chosen:     alt.Group + ":" + alt.Module + ":" + alt.Version,
				Reason:     "android_suffix_fallback",
			})
			coord = alt
			modulePath = r.moduleBasePath(coord)
			moduleFile, _ = findFile(modulePath, ".module")
			if moduleFile == "" {
				if fetched, fetchErr := r.fetchModuleMetadata(coord); fetchErr == nil {
					moduleFile = fetched
				}
			}
			if moduleFile != "" {
				artifact, androidLibrary, deps, err := r.resolveModuleMetadata(coord, moduleFile, r.metadataSourceForPath(moduleFile, "module"), depth)
				if err != nil && errors.Is(err, errAvailableAtDepthExceeded) {
					call.result = resolveResult{err: err}
					return "", nil, nil, err
				}
				if err == nil && (artifact != "" || androidLibrary != nil) {
					deps = applyExcludes(deps, coord.Excludes)
					call.result = resolveResult{artifact: artifact, androidLibrary: androidLibrary, deps: deps}
					return artifact, androidLibrary, deps, nil
				}
			}
			pomFile, _ = findFile(modulePath, ".pom")
			if pomFile == "" {
				if fetched, fetchErr := r.fetchPOM(coord); fetchErr == nil {
					pomFile = fetched
				}
			}
		}
	}
	if pomFile == "" {
		artifact, androidLibrary, artifactErr := r.findArtifact(modulePath)
		if artifactErr == nil {
			r.addReplayPin(ResolutionPin{
				Coordinate: coord.Group + ":" + coord.Module + ":" + coord.Version,
			})
			call.result = resolveResult{artifact: artifact, androidLibrary: androidLibrary}
			return artifact, androidLibrary, nil, nil
		}
		call.result = resolveResult{}
		return "", nil, nil, nil
	}
	deps, err := parsePOMDeps(pomFile)
	if err != nil {
		call.result = resolveResult{err: err}
		return "", nil, nil, err
	}
	deps = applyExcludes(deps, coord.Excludes)
	artifact, androidLibrary, err := r.findArtifact(modulePath)
	if err != nil {
		r.addReplayPin(ResolutionPin{
			Coordinate: coord.Group + ":" + coord.Module + ":" + coord.Version,
		})
		call.result = resolveResult{deps: deps}
		return "", nil, deps, nil
	}
	r.addReplayPin(ResolutionPin{
		Coordinate: coord.Group + ":" + coord.Module + ":" + coord.Version,
	})
	call.result = resolveResult{artifact: artifact, androidLibrary: androidLibrary, deps: deps}
	return artifact, androidLibrary, deps, nil
}

func (r *Resolver) preferJVMSibling(coord Coordinate) (Coordinate, bool) {
	if strings.HasSuffix(coord.Module, "-jvm") || strings.HasSuffix(coord.Module, "-android") {
		return Coordinate{}, false
	}
	rootArtifact, err := findArtifactCandidate(r.moduleBasePath(coord))
	if err == nil && artifactContainsClassFiles(rootArtifact) {
		return Coordinate{}, false
	}
	alt := coord
	alt.Module = coord.Module + "-jvm"
	if normalized, ok := r.normalizeFallbackCoordinate(alt); ok {
		return normalized, true
	}
	return Coordinate{}, false
}

func (r *Resolver) normalizeFallbackCoordinate(coord Coordinate) (Coordinate, bool) {
	if r.moduleBaseHasResolvableContent(r.moduleBasePath(coord)) {
		return coord, true
	}
	if cached := r.findCachedVersion(coord.Group, coord.Module); cached != "" && cached != coord.Version {
		alt := coord
		alt.Version = cached
		if r.moduleBaseHasResolvableContent(r.moduleBasePath(alt)) {
			return alt, true
		}
	}
	return Coordinate{}, false
}

// moduleBaseHasResolvableContent reports whether base contains an actual
// resolvable file (a Gradle .module, a Maven .pom, or a .jar/.aar
// artifact) — not just an empty directory.
//
// A bare os.Stat on the module base is not enough: grit's own failed
// fetch attempts call os.MkdirAll on the `downloaded/` output directory
// before issuing the request, so a coordinate that never resolved still
// leaves an empty directory behind. preferJVMSibling and the android
// suffix fallback used to treat that empty directory as proof that a
// `<module>-jvm` / `<module>-android` sibling exists, which silently
// rerouted real artifacts (e.g. the Android-only `coil-compose` AAR) to
// a non-existent KMP variant and dropped them from the closure.
func (r *Resolver) moduleBaseHasResolvableContent(base string) bool {
	if _, err := findFile(base, ".module"); err == nil {
		return true
	}
	if _, err := findFile(base, ".pom"); err == nil {
		return true
	}
	if _, err := findArtifactCandidate(base); err == nil {
		return true
	}
	return false
}

func artifactContainsClassFiles(path string) bool {
	if !strings.HasSuffix(strings.ToLower(path), ".jar") {
		return true
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return true
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".class") {
			return true
		}
	}
	return false
}

func (r *Resolver) findArtifact(base string) (string, *AndroidLibrary, error) {
	file, err := findArtifactCandidate(base)
	if err != nil {
		coord, coordErr := r.coordinateForBase(base)
		if coordErr != nil {
			return "", nil, err
		}
		file, err = r.fetchArtifactCandidate(coord, base)
		if err != nil {
			return "", nil, err
		}
	}
	return r.normalizeArtifact(file)
}

func (r *Resolver) coordinateForBase(base string) (Coordinate, error) {
	clean := filepath.Clean(base)
	parts := strings.Split(clean, string(os.PathSeparator))
	if len(parts) < 3 {
		return Coordinate{}, fmt.Errorf("artifact base %s does not contain group/module/version", base)
	}
	return Coordinate{
		Group:   parts[len(parts)-3],
		Module:  parts[len(parts)-2],
		Version: parts[len(parts)-1],
	}, nil
}
