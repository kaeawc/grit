package dependencywiring

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/testsupport"
)

func TestResolvePluginVersionFindsAliasAndPluginID(t *testing.T) {
	root := t.TempDir()
	catalogPath := filepath.Join(root, "gradle", "libs.versions.toml")
	writeTestFile(t, catalogPath, `
[versions]
wire = "5.2.0"

[plugins]
square-wire = { id = "com.squareup.wire", version.ref = "wire" }
`)
	prj := &project.Project{RootDir: root, VersionCatalogs: []string{catalogPath}}

	got, err := ResolvePluginVersion(prj, "square.wire")
	if err != nil {
		t.Fatalf("ResolvePluginVersion alias: %v", err)
	}
	if got != "5.2.0" {
		t.Fatalf("alias version = %q, want 5.2.0", got)
	}

	got, err = ResolvePluginVersion(prj, "com.squareup.wire")
	if err != nil {
		t.Fatalf("ResolvePluginVersion plugin id: %v", err)
	}
	if got != "5.2.0" {
		t.Fatalf("plugin id version = %q, want 5.2.0", got)
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRawClasspathUsesResolverAndMergesJars(t *testing.T) {
	resolver := testsupport.NewWiringResolverRecorder()
	resolver.Result = &m2local.Resolved{
		CompileJars: []string{"/deps/tool.jar", "/deps/shared.jar"},
		RuntimeJars: []string{"/deps/shared.jar", "/deps/runtime.jar"},
	}

	got, err := ResolveRawClasspath(resolver, "g:tool:1.0")
	if err != nil {
		t.Fatalf("ResolveRawClasspath: %v", err)
	}
	want := []string{"/deps/tool.jar", "/deps/shared.jar", "/deps/runtime.jar"}
	if len(got) != len(want) {
		t.Fatalf("classpath len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("classpath[%d] = %q, want %q; all=%#v", i, got[i], want[i], got)
		}
	}
	calls := resolver.CallsSnapshot()
	if len(calls) != 1 || len(calls[0].Main) != 1 || calls[0].Main[0].Value != "g:tool:1.0" {
		t.Fatalf("unexpected resolver calls: %#v", calls)
	}
}
