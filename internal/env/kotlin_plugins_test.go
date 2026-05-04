package env

import (
	"os"
	"path/filepath"
	"testing"
)

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
