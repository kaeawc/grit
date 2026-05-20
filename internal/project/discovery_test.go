package project

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyDiscoverySnapshotAddsGeneratedSourceSets(t *testing.T) {
	root := t.TempDir()
	prj := &Project{
		RootDir: root,
		Modules: []Module{{
			Path: ":app",
			Dir:  filepath.Join(root, "app"),
		}},
	}
	snapshot := DiscoverySnapshot{
		SchemaVersion: 1,
		Modules: map[string][]GeneratedSourceSet{
			":app": {{
				Provider: "gradle-discovery",
				Language: "kotlin",
				Dirs:     []string{filepath.Join(root, "app", "build", "generated", "custom")},
			}},
		},
	}
	ApplyDiscoverySnapshot(prj, snapshot)
	mod := prj.FindModule(":app")
	if mod == nil || len(mod.GeneratedSources) != 1 {
		t.Fatalf("expected discovered generated source set, got %#v", mod)
	}
	if !mod.GeneratedSources[0].Discovered {
		t.Fatalf("expected discovered flag on generated source set: %#v", mod.GeneratedSources[0])
	}
}

func TestLoadDiscoverySnapshotToleratesMissingFile(t *testing.T) {
	prj := &Project{RootDir: t.TempDir()}
	snapshot, err := LoadDiscoverySnapshot(prj)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != 0 || len(snapshot.Modules) != 0 {
		t.Fatalf("unexpected missing snapshot value: %#v", snapshot)
	}
}

func TestDiscoverySnapshotPathUsesGritMetadata(t *testing.T) {
	root := t.TempDir()
	prj := &Project{RootDir: root}
	path := DiscoverySnapshotPath(prj)
	if filepath.Dir(path) != filepath.Join(root, ".grit", "metadata", "discovery") {
		t.Fatalf("unexpected discovery path: %q", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryWarningClassifiesContextErrors(t *testing.T) {
	if got := discoveryWarning(context.DeadlineExceeded, errors.New("signal: killed")); !strings.Contains(got, "timed out after") {
		t.Fatalf("expected timeout warning, got %q", got)
	}
	if got := discoveryWarning(context.Canceled, errors.New("signal: interrupt")); !strings.Contains(got, "canceled") {
		t.Fatalf("expected cancellation warning, got %q", got)
	}
	if got := discoveryWarning(nil, errors.New("gradlew failed: boom")); !strings.Contains(got, "gradlew failed: boom") {
		t.Fatalf("expected wrapped run error, got %q", got)
	}
}

func TestRefreshDiscoverySnapshotEmitsHybridWarningOnFailure(t *testing.T) {
	root := t.TempDir()
	gradlew := filepath.Join(root, "gradlew")
	// A wrapper script that always exits non-zero — the hybrid-mode
	// caller must continue but should surface a warning.
	if err := os.WriteFile(gradlew, []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prj := &Project{
		RootDir:          root,
		DiscoveryMode:    "hybrid",
		RefreshDiscovery: true,
	}

	var warnings bytes.Buffer
	if err := RefreshDiscoverySnapshot(context.Background(), prj, &warnings); err != nil {
		t.Fatalf("hybrid mode should swallow the error, got %v", err)
	}
	if !strings.Contains(warnings.String(), "gradle discovery snapshot failed") {
		t.Fatalf("expected warning naming the failure, got %q", warnings.String())
	}
}

func TestRefreshDiscoverySnapshotReturnsErrorInSnapshotMode(t *testing.T) {
	root := t.TempDir()
	gradlew := filepath.Join(root, "gradlew")
	if err := os.WriteFile(gradlew, []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prj := &Project{
		RootDir:          root,
		DiscoveryMode:    "snapshot",
		RefreshDiscovery: true,
	}

	var warnings bytes.Buffer
	err := RefreshDiscoverySnapshot(context.Background(), prj, &warnings)
	if err == nil {
		t.Fatal("snapshot mode must surface failures as errors")
	}
	if warnings.Len() != 0 {
		t.Fatalf("snapshot mode should not write warnings, got %q", warnings.String())
	}
}

func TestRefreshDiscoverySnapshotTolerantWhenGradlewMissing(t *testing.T) {
	root := t.TempDir()
	prj := &Project{RootDir: root, DiscoveryMode: "hybrid"}

	var warnings bytes.Buffer
	if err := RefreshDiscoverySnapshot(context.Background(), prj, &warnings); err != nil {
		t.Fatalf("hybrid mode should tolerate missing gradlew, got %v", err)
	}
	if warnings.Len() != 0 {
		t.Fatalf("absent gradlew shouldn't trigger a warning, got %q", warnings.String())
	}
}

func TestDiscoveryRefreshTimeoutNonZero(t *testing.T) {
	if DiscoveryRefreshTimeout <= 0 {
		t.Fatalf("expected positive discovery timeout, got %s", DiscoveryRefreshTimeout)
	}
}
