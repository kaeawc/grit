package env

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

type fakeArtifactResolver struct {
	resolved *m2local.Resolved
}

func (r fakeArtifactResolver) Resolve(*modulebuild.Dependencies) (*m2local.Resolved, error) {
	return r.resolved, nil
}

func TestKotlincLibDirResolvesAdjacentLib(t *testing.T) {
	tempDir := t.TempDir()
	dist, err := filepath.EvalSymlinks(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dist, "bin")
	lib := filepath.Join(dist, "lib")
	for _, d := range []string{bin, lib} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	kotlincPath := filepath.Join(bin, "kotlinc")
	if err := os.WriteFile(kotlincPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	got := KotlincLibDir()
	if got != lib {
		t.Fatalf("KotlincLibDir = %q want %q", got, lib)
	}
}

func TestLocateComposeCompilerPluginPrefersKotlincLib(t *testing.T) {
	tempDir := t.TempDir()
	dist, err := filepath.EvalSymlinks(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dist, "bin")
	lib := filepath.Join(dist, "lib")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "kotlinc"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	jar := filepath.Join(lib, "compose-compiler-plugin.jar")
	if err := os.WriteFile(jar, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	got := LocateComposeCompilerPlugin()
	if got != jar {
		t.Fatalf("LocateComposeCompilerPlugin = %q want %q", got, jar)
	}
}

func TestCheckKotlinCompilerAcceptsProjectCompilerFromGradleCache(t *testing.T) {
	tempDir := t.TempDir()
	emptyPath := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(emptyPath, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", emptyPath)
	t.Setenv("HOME", tempDir)

	jar := filepath.Join(
		tempDir,
		".gradle", "caches", "modules-2", "files-2.1",
		"org.jetbrains.kotlin", "kotlin-compiler-embeddable", "2.1.20",
		"hash", "kotlin-compiler-embeddable-2.1.20.jar",
	)
	if err := os.MkdirAll(filepath.Dir(jar), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jar, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}

	item := checkKotlinCompiler(&project.Project{
		VersionCatalogData: map[string]string{"build-kotlin": "2.1.20"},
	})
	if !item.OK {
		t.Fatalf("expected cached Kotlin compiler to pass: %#v", item)
	}
	if item.Detail != "using Gradle cache: "+jar {
		t.Fatalf("checkKotlinCompiler detail = %q want cached jar %q", item.Detail, jar)
	}
}

func TestCheckKotlinCompilerFailsWithoutPathOrCachedCompiler(t *testing.T) {
	tempDir := t.TempDir()
	emptyPath := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(emptyPath, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", emptyPath)
	t.Setenv("HOME", tempDir)

	item := checkKotlinCompiler(&project.Project{
		VersionCatalogData: map[string]string{"build-kotlin": "2.1.20"},
	})
	if item.OK {
		t.Fatalf("expected missing Kotlin compiler to fail: %#v", item)
	}
}

func TestCheckMetroCompilerPluginUsesResolverPath(t *testing.T) {
	prj := &project.Project{
		VersionCatalogData: map[string]string{"metro": "0.13.0"},
	}
	resolver := fakeArtifactResolver{
		resolved: &m2local.Resolved{
			RuntimeJars: []string{"/work/.grit/worktree/materialized-m2/dev/zacsweers/metro/compiler/0.13.0/compiler-0.13.0.jar"},
		},
	}

	item := checkMetroCompilerPluginWithResolver(prj, resolver)
	if !item.OK {
		t.Fatalf("expected Metro compiler plugin to pass: %#v", item)
	}
	if item.Detail != "/work/.grit/worktree/materialized-m2/dev/zacsweers/metro/compiler/0.13.0/compiler-0.13.0.jar" {
		t.Fatalf("unexpected detail: %q", item.Detail)
	}
}

func TestCheckMetroCompilerPluginReportsMissingVersion(t *testing.T) {
	item := checkMetroCompilerPluginWithResolver(&project.Project{}, fakeArtifactResolver{})
	if item.OK {
		t.Fatalf("expected missing Metro version to fail: %#v", item)
	}
	if item.Detail != "no Metro version declared" {
		t.Fatalf("unexpected detail: %q", item.Detail)
	}
}
