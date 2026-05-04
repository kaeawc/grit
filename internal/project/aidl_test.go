package project

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestDiscoverAidlFiles(t *testing.T) {
	t.Run("finds aidl files in main source set", func(t *testing.T) {
		dir := t.TempDir()
		aidlDir := filepath.Join(dir, "src", "main", "aidl", "com", "example")
		if err := os.MkdirAll(aidlDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(aidlDir, "IMyService.aidl"), []byte("interface IMyService {}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(aidlDir, "ICallback.aidl"), []byte("interface ICallback {}"), 0o644); err != nil {
			t.Fatal(err)
		}

		files := discoverAidlFiles(dir, []string{"main"})
		sort.Strings(files)

		want := []string{
			filepath.Join("src", "main", "aidl", "com", "example", "ICallback.aidl"),
			filepath.Join("src", "main", "aidl", "com", "example", "IMyService.aidl"),
		}
		if len(files) != len(want) {
			t.Fatalf("got %d files, want %d: %v", len(files), len(want), files)
		}
		for i := range want {
			if files[i] != want[i] {
				t.Errorf("files[%d] = %q, want %q", i, files[i], want[i])
			}
		}
	})

	t.Run("scans multiple source sets", func(t *testing.T) {
		dir := t.TempDir()
		for _, ss := range []string{"main", "debug"} {
			aidlDir := filepath.Join(dir, "src", ss, "aidl")
			if err := os.MkdirAll(aidlDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(aidlDir, ss+".aidl"), []byte("interface {}"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		files := discoverAidlFiles(dir, []string{"main", "debug"})
		if len(files) != 2 {
			t.Fatalf("got %d files, want 2: %v", len(files), files)
		}
	})

	t.Run("ignores non-aidl files", func(t *testing.T) {
		dir := t.TempDir()
		aidlDir := filepath.Join(dir, "src", "main", "aidl")
		if err := os.MkdirAll(aidlDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(aidlDir, "IService.aidl"), []byte("interface IService {}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(aidlDir, "README.md"), []byte("docs"), 0o644); err != nil {
			t.Fatal(err)
		}

		files := discoverAidlFiles(dir, []string{"main"})
		if len(files) != 1 {
			t.Fatalf("got %d files, want 1: %v", len(files), files)
		}
		if files[0] != filepath.Join("src", "main", "aidl", "IService.aidl") {
			t.Errorf("got %q, want aidl file", files[0])
		}
	})

	t.Run("returns nil when no aidl directory exists", func(t *testing.T) {
		dir := t.TempDir()
		files := discoverAidlFiles(dir, []string{"main"})
		if files != nil {
			t.Fatalf("got %v, want nil", files)
		}
	})

	t.Run("returns nil for empty source sets", func(t *testing.T) {
		dir := t.TempDir()
		files := discoverAidlFiles(dir, nil)
		if files != nil {
			t.Fatalf("got %v, want nil", files)
		}
	})
}
