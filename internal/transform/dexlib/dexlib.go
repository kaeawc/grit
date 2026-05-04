// Package dexlib implements the dex-external-library Layer 4 transform.
//
// The transform takes one input blob (a library JAR) and produces one or
// more dex file blobs. Pre-dexing external libraries independently allows
// their dex output to be cached and reused across builds, since external
// libraries change infrequently compared to project code.
//
// The action hash is derived from the input JAR content hash and the d8
// tool version, so a cached dex result is reused whenever the same JAR is
// dexed with the same d8 version.
package dexlib

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/transform"
)

const (
	// Kind is the transform action kind recorded in action hashes and
	// provenance for this transform.
	Kind = "dex-external-library"
	// Tool is the stable tool identifier for this transform implementation.
	Tool = "d8"
	// ToolVersion is bumped when the dexing logic changes in a way that
	// should invalidate cached outputs. This is separate from the d8
	// binary version which is passed as an argument to the action.
	ToolVersion = "1"

	// RoleJARInput is the input role for the source library JAR blob.
	RoleJARInput = "jar"
	// RoleDex is the output role prefix for produced dex file(s).
	// When a library is partitioned into multiple dex files, they are
	// numbered: "dex-0", "dex-1", etc.
	RoleDex = "dex"
)

// Action returns the transform action for dexing jarHash with the given
// d8 binary version. The returned Action's Hash is the cache key.
func Action(jarHash cas.Hash, d8Version string) transform.Action {
	return transform.Action{
		Kind:        Kind,
		Tool:        Tool,
		ToolVersion: ToolVersion,
		Args:        []string{"--d8-version", d8Version},
		Inputs: []transform.Input{
			{Role: RoleJARInput, Hash: jarHash},
		},
		Outputs: []transform.OutputDecl{
			{Role: RoleDex, Kind: "dex"},
		},
	}
}

// dexOutputRole returns the output role for the i-th dex partition.
func dexOutputRole(index int) string {
	return RoleDex + "-" + strconv.Itoa(index)
}

// Dex runs the dex-external-library transform against the input JAR blob
// in store. It returns the action result, serving a cached result from
// store.GetActionResult if one exists. Dex is idempotent.
//
// The dexBytes function performs the actual d8 invocation, accepting JAR
// bytes and returning one or more dex file byte slices. Callers provide
// this so the transform is testable without a real d8 binary.
func Dex(ctx context.Context, store cas.Store, jarHash cas.Hash, d8Version string, dexBytes func(jar []byte) ([][]byte, error)) (cas.ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return cas.ActionResult{}, err
	}
	action := Action(jarHash, d8Version)
	actionHash := action.Hash()

	cached, err := store.GetActionResult(ctx, actionHash)
	if err == nil {
		return cached, nil
	}
	if !errors.Is(err, cas.ErrNotFound) {
		return cas.ActionResult{}, err
	}

	rc, err := store.Get(ctx, jarHash)
	if err != nil {
		return cas.ActionResult{}, fmt.Errorf("dexlib: read input blob %s: %w", jarHash, err)
	}
	data, readErr := io.ReadAll(rc)
	closeErr := rc.Close()
	if readErr != nil {
		return cas.ActionResult{}, readErr
	}
	if closeErr != nil {
		return cas.ActionResult{}, closeErr
	}

	dexFiles, err := dexBytes(data)
	if err != nil {
		return cas.ActionResult{}, fmt.Errorf("dexlib: d8 invocation failed: %w", err)
	}
	if len(dexFiles) == 0 {
		return cas.ActionResult{}, fmt.Errorf("dexlib: d8 produced no dex output")
	}

	outputs := make([]cas.NamedOutput, 0, len(dexFiles))
	for i, dex := range dexFiles {
		role := dexOutputRole(i)
		info, err := store.PutBytes(ctx, dex, provenance(jarHash, actionHash, role))
		if err != nil {
			return cas.ActionResult{}, err
		}
		outputs = append(outputs, cas.NamedOutput{Role: role, Blob: info})
	}

	result := cas.ActionResult{ActionHash: actionHash, Outputs: outputs}
	if err := store.PutActionResult(ctx, result); err != nil {
		return cas.ActionResult{}, err
	}
	return result, nil
}

func provenance(jarHash, actionHash cas.Hash, role string) cas.Provenance {
	return cas.Provenance{
		Source: cas.Source{
			Kind: cas.SourceTransform,
			Transform: &cas.TransformSource{
				ActionHash:  actionHash,
				ActionKind:  Kind,
				Tool:        Tool,
				ToolVersion: ToolVersion,
				Inputs: []cas.TransformInput{
					{Role: RoleJARInput, Hash: jarHash},
				},
			},
		},
		Attributes: map[string]string{
			"output.role": role,
		},
	}
}
