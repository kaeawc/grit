package nativecompile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

func (c *Compiler) compileExternalAndroidLibraries(ctx context.Context, libs []m2local.AndroidLibrary, state *compileState, stdout, stderr *os.File) ([]androidResourceArtifact, error) {
	var out []androidResourceArtifact
	for _, lib := range libs {
		if lib.ID == "" {
			continue
		}
		state.mu.Lock()
		cached, ok := state.externalResources[lib.ID]
		state.mu.Unlock()
		if ok {
			out = append(out, cached)
			continue
		}
		artifact := androidResourceArtifact{
			ModulePath:   lib.ID,
			ManifestPath: lib.ManifestPath,
		}
		if lib.ResDir != "" {
			outRoot := filepath.Join(sharedNativeCacheRoot(), "resources", "external", sanitizePathComponent(lib.ID))
			compiledDir := filepath.Join(outRoot, "compiled")
			compiledStamp := filepath.Join(outRoot, "compiled.stamp")
			if err := os.MkdirAll(compiledDir, 0o755); err != nil {
				return nil, err
			}
			if !outputsNewerThanInputs(compiledStamp, []string{lib.ResDir}) {
				if ensureStampFromOutput(compiledStamp, compiledDir, []string{lib.ResDir}) {
					artifact.CompiledStamp = compiledStamp
				} else {
					if err := runAAPT2Compile(ctx, lib.ResDir, compiledDir, stdout, stderr); err != nil {
						return nil, err
					}
					if err := touchFile(compiledStamp); err != nil {
						return nil, err
					}
				}
			}
			if artifact.CompiledStamp == "" {
				artifact.CompiledStamp = compiledStamp
			}
			compiledFiles, err := filepath.Glob(filepath.Join(compiledDir, "*.flat"))
			if err != nil {
				return nil, err
			}
			sort.Strings(compiledFiles)
			artifact.CompiledDir = compiledDir
			artifact.CompiledFiles = compiledFiles
		}
		state.mu.Lock()
		state.externalResources[lib.ID] = artifact
		state.mu.Unlock()
		out = append(out, artifact)
	}
	return uniqueResourceArtifacts(out), nil
}

func (c *Compiler) resolveProjectDeps(ctx context.Context, prj *project.Project, parentKey string, compileRefs, runtimeRefs []modulebuild.Ref, variantName string, state *compileState, ancestry map[string]bool, stdout, stderr *os.File) ([]string, []string, []androidResourceArtifact, error) {
	seen := map[string]bool{}
	compileNeeded := map[string]bool{}
	runtimeNeeded := map[string]bool{}
	var modules []*project.Module
	var childKeys []string
	for _, ref := range compileRefs {
		if ref.Kind != "project" {
			continue
		}
		compileNeeded[ref.Value] = true
		if seen[ref.Value] {
			continue
		}
		seen[ref.Value] = true
		mod := prj.FindModule(ref.Value)
		if mod == nil {
			return nil, nil, nil, fmt.Errorf("project dependency %s not found", ref.Value)
		}
		modules = append(modules, mod)
		childKeys = append(childKeys, mod.Path+"#"+variantName)
	}
	for _, ref := range runtimeRefs {
		if ref.Kind != "project" {
			continue
		}
		runtimeNeeded[ref.Value] = true
		if seen[ref.Value] {
			continue
		}
		seen[ref.Value] = true
		mod := prj.FindModule(ref.Value)
		if mod == nil {
			return nil, nil, nil, fmt.Errorf("project dependency %s not found", ref.Value)
		}
		modules = append(modules, mod)
		childKeys = append(childKeys, mod.Path+"#"+variantName)
	}
	if parentKey != "" {
		if err := state.addProjectDeps(parentKey, childKeys); err != nil {
			return nil, nil, nil, err
		}
	}
	type projectDepResult struct {
		classesDir     string
		runtimeInputs  []string
		resourceInputs []androidResourceArtifact
		err            error
	}
	results := make([]projectDepResult, len(modules))
	var wg sync.WaitGroup
	for i, mod := range modules {
		wg.Add(1)
		go func(index int, child *project.Module) {
			defer wg.Done()
			classesDir, childRuntime, childResources, err := c.compileMainInternal(ctx, prj, child, variantName, state, cloneAncestry(ancestry), stdout, stderr)
			results[index] = projectDepResult{
				classesDir:     classesDir,
				runtimeInputs:  childRuntime,
				resourceInputs: childResources,
				err:            err,
			}
		}(i, mod)
	}
	wg.Wait()
	var compileCP []string
	var runtimeInputs []string
	var androidResources []androidResourceArtifact
	for i, result := range results {
		if result.err != nil {
			return nil, nil, nil, result.err
		}
		mod := modules[i]
		if compileNeeded[mod.Path] {
			compileCP = append(compileCP, result.runtimeInputs...)
			for _, resource := range result.resourceInputs {
				if resource.SymbolJar != "" {
					compileCP = append(compileCP, resource.SymbolJar)
				}
			}
		}
		if runtimeNeeded[mod.Path] {
			runtimeInputs = append(runtimeInputs, result.runtimeInputs...)
			androidResources = append(androidResources, result.resourceInputs...)
		}
	}
	return mergePaths(compileCP), mergePaths(runtimeInputs), uniqueResourceArtifacts(androidResources), nil
}
