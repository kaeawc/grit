// Package aarextract implements the aar-extract Layer 4 transform.
//
// The transform takes one input blob (an Android Archive) and produces
// named output blobs for the archive's classes.jar, AndroidManifest.xml,
// and a deterministic zip of the res/ subtree when present.
// The transform is deterministic and cached by action hash: the second
// Extract call for the same AAR hash is served from the action-result
// index without re-reading the archive bytes.
package aarextract

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/transform"
)

const (
	// Kind is the transform action kind recorded in action hashes and
	// provenance for this transform.
	Kind = "aar-extract"
	// Tool is the stable tool identifier for this transform implementation.
	Tool = "grit-aar-extract"
	// ToolVersion is bumped when the extraction logic changes in a way
	// that should invalidate cached outputs. Bumping this is the deliberate
	// way to force re-extraction across the fleet.
	ToolVersion = "2"

	// RoleAARInput is the input role for the source AAR blob.
	RoleAARInput = "aar"
	// RoleClassesJar is the output role for the extracted classes.jar.
	RoleClassesJar = "classes-jar"
	// RoleAndroidManifest is the output role for the extracted manifest.
	RoleAndroidManifest = "android-manifest"
	// RoleResourceTree is the output role for the normalized res/ subtree.
	RoleResourceTree = "resource-tree"
)

var reproducibleZipTime = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

// Action returns the transform action for extracting aarHash. The returned
// Action's Hash is the cache key.
func Action(aarHash cas.Hash) transform.Action {
	return transform.Action{
		Kind:        Kind,
		Tool:        Tool,
		ToolVersion: ToolVersion,
		Inputs: []transform.Input{
			{Role: RoleAARInput, Hash: aarHash},
		},
		Outputs: []transform.OutputDecl{
			{Role: RoleClassesJar, Kind: "jar"},
			{Role: RoleAndroidManifest, Kind: "xml"},
			{Role: RoleResourceTree, Kind: "zip"},
		},
	}
}

// Extract runs the aar-extract transform against the input AAR blob in
// store. It returns the action result, serving a cached result from
// store.GetActionResult if one exists. Extract is idempotent.
func Extract(ctx context.Context, store cas.Store, aarHash cas.Hash) (cas.ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return cas.ActionResult{}, err
	}
	action := Action(aarHash)
	actionHash := action.Hash()

	cached, err := store.GetActionResult(ctx, actionHash)
	if err == nil {
		return cached, nil
	}
	if !errors.Is(err, cas.ErrNotFound) {
		return cas.ActionResult{}, err
	}

	rc, err := store.Get(ctx, aarHash)
	if err != nil {
		return cas.ActionResult{}, fmt.Errorf("aarextract: read input blob %s: %w", aarHash, err)
	}
	data, readErr := io.ReadAll(rc)
	closeErr := rc.Close()
	if readErr != nil {
		return cas.ActionResult{}, readErr
	}
	if closeErr != nil {
		return cas.ActionResult{}, closeErr
	}

	result, err := extractFromBytes(ctx, store, aarHash, actionHash, data)
	if err != nil {
		return cas.ActionResult{}, err
	}
	if err := store.PutActionResult(ctx, result); err != nil {
		return cas.ActionResult{}, err
	}
	return result, nil
}

func extractFromBytes(ctx context.Context, store cas.Store, aarHash, actionHash cas.Hash, data []byte) (cas.ActionResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return cas.ActionResult{}, fmt.Errorf("aarextract: open zip: %w", err)
	}

	var classesJar []byte
	var manifest []byte
	resourceEntries := map[string][]byte{}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		switch f.Name {
		case "classes.jar":
			body, err := readZipEntry(f)
			if err != nil {
				return cas.ActionResult{}, fmt.Errorf("aarextract: read classes.jar: %w", err)
			}
			classesJar = body
		case "AndroidManifest.xml":
			body, err := readZipEntry(f)
			if err != nil {
				return cas.ActionResult{}, fmt.Errorf("aarextract: read AndroidManifest.xml: %w", err)
			}
			manifest = body
		default:
			if len(f.Name) > len("res/") && f.Name[:len("res/")] == "res/" {
				body, err := readZipEntry(f)
				if err != nil {
					return cas.ActionResult{}, fmt.Errorf("aarextract: read %s: %w", f.Name, err)
				}
				resourceEntries[f.Name] = body
			}
		}
	}

	var outputs []cas.NamedOutput

	if classesJar != nil {
		info, err := store.PutBytes(ctx, classesJar, provenance(aarHash, actionHash, RoleClassesJar))
		if err != nil {
			return cas.ActionResult{}, err
		}
		outputs = append(outputs, cas.NamedOutput{Role: RoleClassesJar, Blob: info})
	}
	if manifest != nil {
		info, err := store.PutBytes(ctx, manifest, provenance(aarHash, actionHash, RoleAndroidManifest))
		if err != nil {
			return cas.ActionResult{}, err
		}
		outputs = append(outputs, cas.NamedOutput{Role: RoleAndroidManifest, Blob: info})
	}
	if len(resourceEntries) != 0 {
		resourceTree, err := normalizedResourceTreeZip(resourceEntries)
		if err != nil {
			return cas.ActionResult{}, err
		}
		info, err := store.PutBytes(ctx, resourceTree, provenance(aarHash, actionHash, RoleResourceTree))
		if err != nil {
			return cas.ActionResult{}, err
		}
		outputs = append(outputs, cas.NamedOutput{Role: RoleResourceTree, Blob: info})
	}

	return cas.ActionResult{ActionHash: actionHash, Outputs: outputs}, nil
}

func normalizedResourceTreeZip(entries map[string][]byte) ([]byte, error) {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range names {
		header := &zip.FileHeader{
			Name:   name,
			Method: zip.Store,
		}
		header.SetModTime(reproducibleZipTime)
		w, err := zw.CreateHeader(header)
		if err != nil {
			return nil, fmt.Errorf("aarextract: create normalized resource entry %s: %w", name, err)
		}
		if _, err := w.Write(entries[name]); err != nil {
			return nil, fmt.Errorf("aarextract: write normalized resource entry %s: %w", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("aarextract: close normalized resource zip: %w", err)
	}
	return buf.Bytes(), nil
}

func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

func provenance(aarHash, actionHash cas.Hash, role string) cas.Provenance {
	return cas.Provenance{
		Source: cas.Source{
			Kind: cas.SourceTransform,
			Transform: &cas.TransformSource{
				ActionHash:  actionHash,
				ActionKind:  Kind,
				Tool:        Tool,
				ToolVersion: ToolVersion,
				Inputs: []cas.TransformInput{
					{Role: RoleAARInput, Hash: aarHash},
				},
			},
		},
		Attributes: map[string]string{
			"output.role": role,
		},
	}
}
