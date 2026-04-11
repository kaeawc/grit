package nativecompile

import (
	"context"
	"fmt"
	"os"

	"github.com/kaeawc/grit/internal/project"
)

func (c *Compiler) compileMainInternal(ctx context.Context, prj *project.Project, mod *project.Module, variantName string, state *compileState, ancestry map[string]bool, stdout, stderr *os.File) (string, []string, []androidResourceArtifact, error) {
	key := mod.Path + "#" + variantName
	if ancestry[key] {
		return "", nil, nil, fmt.Errorf("cyclic project dependency while compiling %s", mod.Path)
	}
	state.mu.Lock()
	if out, ok := state.outputs[key]; ok {
		state.mu.Unlock()
		return out.classesDir, out.runtimeInputs, out.androidResources, nil
	}
	if call, ok := state.inFlight[key]; ok {
		state.mu.Unlock()
		<-call.done
		state.mu.Lock()
		out, ok := state.outputs[key]
		state.mu.Unlock()
		if !ok {
			if call.err == nil {
				call.err = fmt.Errorf("compilation for %s completed without output", mod.Path)
			}
			return "", nil, nil, call.err
		}
		return out.classesDir, out.runtimeInputs, out.androidResources, call.err
	}
	call := &inFlightCompile{done: make(chan struct{})}
	state.inFlight[key] = call
	state.mu.Unlock()
	defer func() {
		state.mu.Lock()
		delete(state.inFlight, key)
		state.mu.Unlock()
		close(call.done)
	}()
	childAncestry := cloneAncestry(ancestry)
	childAncestry[key] = true
	if out, ok := c.tryCompileMainCacheHit(prj, mod, variantName, key, state); ok {
		return out.classesDir, out.runtimeInputs, out.androidResources, nil
	}
	resolvedDeps, err := c.resolveMainDependencies(ctx, prj, mod, variantName, key, state, childAncestry, stdout, stderr)
	if err != nil {
		call.err = err
		return "", nil, nil, err
	}
	prepared, err := c.prepareMainCompile(ctx, prj, mod, variantName, state, resolvedDeps, stdout, stderr)
	if err != nil {
		call.err = err
		return "", nil, nil, err
	}
	if len(prepared.mainSources) == 0 {
		mainOut, runtimeInputs, androidResources, err := c.finishMainWithoutSources(prj, mod, variantName, key, state, resolvedDeps, prepared)
		call.err = err
		return mainOut, runtimeInputs, androidResources, err
	}
	mainOut, runtimeInputs, androidResources, err := c.compileMainSources(ctx, prj, mod, variantName, key, state, resolvedDeps, prepared, stdout, stderr)
	call.err = err
	return mainOut, runtimeInputs, androidResources, err
}
