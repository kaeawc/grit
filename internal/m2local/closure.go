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
	var jars []string
	var androidLibraries []AndroidLibrary
	queue := append([]Coordinate{}, roots...)

	for len(queue) > 0 {
		frontier := append([]Coordinate{}, queue...)
		queue = nil
		var pending []Coordinate
		for _, coord := range frontier {
			key := coord.Group + ":" + coord.Module + ":" + coord.Version
			moduleKey := coord.Group + ":" + coord.Module
			if selected, ok := selectedByModule[moduleKey]; ok && selected != coord.Version {
				r.addConflict(ResolutionConflict{
					Kind:        "version_conflict",
					Module:      moduleKey,
					Selected:    selected,
					Discarded:   coord.Version,
					Coordinates: []string{moduleKey + ":" + selected, moduleKey + ":" + coord.Version},
				})
			} else if !ok {
				selectedByModule[moduleKey] = coord.Version
			}
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
		for _, result := range results {
			if result.err != nil {
				return nil, nil, result.err
			}
			if result.artifact != "" {
				jars = append(jars, result.artifact)
			}
			if result.androidLibrary != nil && (result.androidLibrary.ManifestPath != "" || result.androidLibrary.ResDir != "") {
				androidLibraries = append(androidLibraries, *result.androidLibrary)
			}
			queue = append(queue, result.deps...)
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
		artifact, androidLibrary, deps, err := r.resolveModuleMetadata(coord, moduleFile, r.metadataSourceForPath(moduleFile, "module"))
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
				artifact, androidLibrary, deps, err := r.resolveModuleMetadata(coord, moduleFile, r.metadataSourceForPath(moduleFile, "module"))
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
				artifact, androidLibrary, deps, err := r.resolveModuleMetadata(coord, moduleFile, r.metadataSourceForPath(moduleFile, "module"))
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
				artifact, androidLibrary, deps, err := r.resolveModuleMetadata(coord, moduleFile, r.metadataSourceForPath(moduleFile, "module"))
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
	if _, err := os.Stat(r.moduleBasePath(coord)); err == nil {
		return coord, true
	}
	if cached := r.findCachedVersion(coord.Group, coord.Module); cached != "" && cached != coord.Version {
		alt := coord
		alt.Version = cached
		if _, err := os.Stat(r.moduleBasePath(alt)); err == nil {
			return alt, true
		}
	}
	return Coordinate{}, false
}

func artifactContainsClassFiles(path string) bool {
	if !strings.HasSuffix(strings.ToLower(path), ".jar") {
		return true
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return true
	}
	defer zr.Close()
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
