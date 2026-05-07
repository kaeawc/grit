package project

import (
	"os"
	"path/filepath"
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
