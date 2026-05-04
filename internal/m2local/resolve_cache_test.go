package m2local

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/modulebuild"
)

func TestResolverSaveResolvedCacheWritesVersionedEnvelope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cacheRoot := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
	workRoot := t.TempDir()
	resolver := New(cacheRoot, workRoot, nil, nil)

	jarPath := filepath.Join(workRoot, "demo.jar")
	if err := os.WriteFile(jarPath, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := &modulebuild.Dependencies{}
	resolved := &Resolved{
		CompileJars: []string{jarPath},
		RuntimeJars: []string{jarPath},
		TestJars:    []string{jarPath},
		Report: ResolutionReport{
			Selections: []ResolutionSelection{{
				Kind:       "variant_selection",
				Coordinate: "g:m:1.0.0",
				Chosen:     "releaseRuntimeElements",
				Reason:     "best_scored_variant",
			}},
		},
		Replay: ResolutionReplay{
			Pins: []ResolutionPin{{
				Coordinate:   "g:m:1.0.0",
				Variant:      "releaseRuntimeElements",
				Capabilities: []string{"g:m-runtime:1.0.0"},
			}},
		},
		Lockfile: ResolutionLockfile{
			SchemaVersion: 1,
			Format:        "m2local-lockfile",
			Pins: []ResolutionPin{{
				Coordinate: "should-be-overwritten",
			}},
		},
	}
	if err := resolver.saveResolvedCache(deps, resolved); err != nil {
		t.Fatal(err)
	}

	path, err := resolver.resolvedCachePath(deps)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope ResolvedEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.SchemaVersion != 1 || envelope.Format != "m2local-resolved" {
		t.Fatalf("unexpected envelope metadata: %#v", envelope)
	}
	if envelope.Topology.SchemaVersion != 1 || envelope.Topology.SharedResolutionRoot == "" || envelope.Topology.SharedAARRoot == "" {
		t.Fatalf("expected cache topology in envelope, got %#v", envelope.Topology)
	}
	if len(envelope.Resolved.CompileJars) != 1 || envelope.Resolved.CompileJars[0] != jarPath {
		t.Fatalf("unexpected resolved payload in envelope: %#v", envelope.Resolved)
	}
	if len(envelope.Resolved.Report.Selections) != 1 || envelope.Resolved.Report.Selections[0].Chosen != "releaseRuntimeElements" {
		t.Fatalf("expected persisted resolution report, got %#v", envelope.Resolved.Report)
	}
	if len(envelope.Resolved.Replay.Pins) != 1 || envelope.Resolved.Replay.Pins[0].Variant != "releaseRuntimeElements" {
		t.Fatalf("expected persisted replay pins, got %#v", envelope.Resolved.Replay)
	}
	if envelope.Resolved.Lockfile.SchemaVersion != 1 || envelope.Resolved.Lockfile.Format != "m2local-lockfile" {
		t.Fatalf("expected persisted lockfile metadata, got %#v", envelope.Resolved.Lockfile)
	}
	if len(envelope.Resolved.Lockfile.Pins) != 1 || envelope.Resolved.Lockfile.Pins[0].Coordinate != "g:m:1.0.0" {
		t.Fatalf("expected persisted lockfile pins, got %#v", envelope.Resolved.Lockfile)
	}
}

func TestResolverLoadResolvedCacheSupportsEnvelopeFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cacheRoot := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
	workRoot := t.TempDir()
	resolver := New(cacheRoot, workRoot, nil, nil)

	jarPath := filepath.Join(workRoot, "demo.jar")
	if err := os.WriteFile(jarPath, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := &modulebuild.Dependencies{}
	path, err := resolver.resolvedCachePath(deps)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(ResolvedEnvelope{
		SchemaVersion: 1,
		Format:        "m2local-resolved",
		Topology:      resolver.Topology(),
		Resolved: Resolved{
			CompileJars: []string{jarPath},
			RuntimeJars: []string{jarPath},
			TestJars:    []string{jarPath},
			Report: ResolutionReport{
				Conflicts: []ResolutionConflict{{
					Kind:      "version_conflict",
					Module:    "g:m",
					Selected:  "1.0.0",
					Discarded: "2.0.0",
				}},
			},
			Replay: ResolutionReplay{
				Pins: []ResolutionPin{{
					Coordinate: "g:m:1.0.0",
				}},
			},
			Lockfile: ResolutionLockfile{
				SchemaVersion: 1,
				Format:        "m2local-lockfile",
				Pins: []ResolutionPin{{
					Coordinate: "g:m:1.0.0",
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := resolver.loadResolvedCache(deps)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || len(loaded.RuntimeJars) != 1 || loaded.RuntimeJars[0] != jarPath {
		t.Fatalf("unexpected loaded resolved envelope: %#v", loaded)
	}
	if len(loaded.Report.Conflicts) != 1 || loaded.Report.Conflicts[0].Kind != "version_conflict" {
		t.Fatalf("expected loaded report conflicts, got %#v", loaded.Report)
	}
	if len(loaded.Replay.Pins) != 1 || loaded.Replay.Pins[0].Coordinate != "g:m:1.0.0" {
		t.Fatalf("expected loaded replay pins, got %#v", loaded.Replay)
	}
	if len(loaded.Lockfile.Pins) != 1 || loaded.Lockfile.Pins[0].Coordinate != "g:m:1.0.0" {
		t.Fatalf("expected loaded lockfile pins, got %#v", loaded.Lockfile)
	}
}

func TestResolverLoadResolvedCacheDerivesLockfileWhenAbsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cacheRoot := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
	workRoot := t.TempDir()
	resolver := New(cacheRoot, workRoot, nil, nil)

	jarPath := filepath.Join(workRoot, "demo.jar")
	if err := os.WriteFile(jarPath, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := &modulebuild.Dependencies{}
	path, err := resolver.resolvedCachePath(deps)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(ResolvedEnvelope{
		SchemaVersion: 1,
		Format:        "m2local-resolved",
		Topology:      resolver.Topology(),
		Resolved: Resolved{
			CompileJars: []string{jarPath},
			RuntimeJars: []string{jarPath},
			TestJars:    []string{jarPath},
			Report: ResolutionReport{
				Selections: []ResolutionSelection{{
					Kind:       "module_redirect",
					Coordinate: "g:m:1.0.0",
					Chosen:     "g:m-jvm:1.0.0",
					Reason:     "prefer_jvm_sibling",
				}},
			},
			Replay: ResolutionReplay{
				Pins: []ResolutionPin{{
					Coordinate: "g:m-jvm:1.0.0",
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := resolver.loadResolvedCache(deps)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Lockfile.SchemaVersion != 1 || loaded.Lockfile.Format != "m2local-lockfile" {
		t.Fatalf("expected derived lockfile metadata, got %#v", loaded.Lockfile)
	}
	if len(loaded.Lockfile.Pins) != 1 || loaded.Lockfile.Pins[0].Coordinate != "g:m-jvm:1.0.0" {
		t.Fatalf("expected derived lockfile pins, got %#v", loaded.Lockfile)
	}
	if len(loaded.Lockfile.Selections) != 1 || loaded.Lockfile.Selections[0].Kind != "module_redirect" {
		t.Fatalf("expected derived lockfile selections, got %#v", loaded.Lockfile)
	}
}
