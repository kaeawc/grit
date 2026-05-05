package nativecompile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJavaMainArgsUsesAppCDSByDefault(t *testing.T) {
	t.Setenv("GRIT_JVM_STARTUP_MODE", "")
	javaDir := t.TempDir()
	javaPath := filepath.Join(javaDir, "java")
	if err := os.WriteFile(javaPath, []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", javaDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cpEntry := filepath.Join(t.TempDir(), "lib.jar")
	if err := os.WriteFile(cpEntry, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := javaMainArgs([]string{cpEntry}, "example.Main", []string{"arg1"})
	if err != nil {
		t.Fatalf("javaMainArgs: %v", err)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-XX:+AutoCreateSharedArchive") {
		t.Fatalf("expected AppCDS flags in args: %v", got)
	}
	if !strings.Contains(joined, "-XX:SharedArchiveFile=") {
		t.Fatalf("expected shared archive flag in args: %v", got)
	}
	if !strings.Contains(joined, "example.Main") {
		t.Fatalf("expected main class in args: %v", got)
	}
}

func TestJavaMainArgsDisablesStartupFlagsWhenOff(t *testing.T) {
	t.Setenv("GRIT_JVM_STARTUP_MODE", "off")
	got, err := javaMainArgs([]string{"lib.jar"}, "example.Main", nil)
	if err != nil {
		t.Fatalf("javaMainArgs: %v", err)
	}
	joined := strings.Join(got, " ")
	if strings.Contains(joined, "SharedArchiveFile") || strings.Contains(joined, "AutoCreateSharedArchive") {
		t.Fatalf("did not expect startup flags in args: %v", got)
	}
}

func TestJavaMainArgsSkipsAppCDSForNonEmptyClasspathDirInAutoMode(t *testing.T) {
	t.Setenv("GRIT_JVM_STARTUP_MODE", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Example.class"), []byte("class"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := javaMainArgs([]string{dir}, "example.Main", nil)
	if err != nil {
		t.Fatalf("javaMainArgs: %v", err)
	}
	joined := strings.Join(got, " ")
	if strings.Contains(joined, "SharedArchiveFile") || strings.Contains(joined, "AutoCreateSharedArchive") {
		t.Fatalf("did not expect AppCDS startup flags for classpath dir: %v", got)
	}
}

func TestSharedArchiveFileArgExtractsArchivePath(t *testing.T) {
	got, ok := sharedArchiveFileArg([]string{
		"-Xshare:auto",
		"-XX:+AutoCreateSharedArchive",
		"-XX:SharedArchiveFile=/tmp/example.jsa",
		"-cp",
		"lib.jar",
	})
	if !ok || got != "/tmp/example.jsa" {
		t.Fatalf("unexpected archive parse result: ok=%v path=%q", ok, got)
	}
}

func TestPrepareJavaStartupArgsCreatesArchiveDir(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "nested", "appcds", "archive.jsa")
	if err := prepareJavaStartupArgs([]string{"-XX:SharedArchiveFile=" + archive}); err != nil {
		t.Fatalf("prepareJavaStartupArgs: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(archive)); err != nil {
		t.Fatalf("expected archive dir to exist: %v", err)
	}
}

func TestAppCDSArchivePathIsDeterministic(t *testing.T) {
	t.Setenv("GRIT_JVM_STARTUP_MODE", "")
	javaDir := t.TempDir()
	javaPath := filepath.Join(javaDir, "java")
	if err := os.WriteFile(javaPath, []byte("fake-java"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", javaDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cpEntry := filepath.Join(t.TempDir(), "lib.jar")
	if err := os.WriteFile(cpEntry, []byte("jar-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	cp := []string{cpEntry}
	path1 := appCDSArchivePath(cp, "com.example.Main")
	path2 := appCDSArchivePath(cp, "com.example.Main")
	if path1 != path2 {
		t.Fatalf("same inputs produced different archive paths:\n  %s\n  %s", path1, path2)
	}

	pathOther := appCDSArchivePath(cp, "com.example.Other")
	if path1 == pathOther {
		t.Fatal("different main classes produced the same archive path")
	}
}

func TestConfiguredJVMStartupMode(t *testing.T) {
	tests := []struct {
		env  string
		want jvmStartupMode
	}{
		{"", jvmStartupAuto},
		{"auto", jvmStartupAuto},
		{"AUTO", jvmStartupAuto},
		{"appcds", jvmStartupAppCDS},
		{"AppCDS", jvmStartupAppCDS},
		{"crac", jvmStartupCRaC},
		{"off", jvmStartupOff},
		{"  off  ", jvmStartupOff},
		{"unknown", jvmStartupAuto},
	}
	for _, tt := range tests {
		t.Run("env="+tt.env, func(t *testing.T) {
			t.Setenv("GRIT_JVM_STARTUP_MODE", tt.env)
			if got := configuredJVMStartupMode(); got != tt.want {
				t.Errorf("configuredJVMStartupMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEvictStaleAppCDSArchives(t *testing.T) {
	dir := t.TempDir()

	// Create a fresh .jsa file (should be kept).
	freshPath := filepath.Join(dir, "fresh.jsa")
	if err := os.WriteFile(freshPath, []byte("fresh"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a stale .jsa file (should be removed).
	stalePath := filepath.Join(dir, "stale.jsa")
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleTime := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(stalePath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	// Create a non-.jsa file with old mtime (should be kept).
	otherPath := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(otherPath, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(otherPath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	removed := evictStaleAppCDSArchives(dir, 7*24*time.Hour)
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatal("fresh.jsa should still exist")
	}
	if _, err := os.Stat(stalePath); err == nil {
		t.Fatal("stale.jsa should have been removed")
	}
	if _, err := os.Stat(otherPath); err != nil {
		t.Fatal("keep.txt should still exist")
	}
}

func TestEvictStaleAppCDSArchivesEmptyDir(t *testing.T) {
	removed := evictStaleAppCDSArchives(t.TempDir(), 7*24*time.Hour)
	if removed != 0 {
		t.Fatalf("expected 0 removed from empty dir, got %d", removed)
	}
}

func TestEvictStaleAppCDSArchivesNonexistentDir(t *testing.T) {
	removed := evictStaleAppCDSArchives(filepath.Join(t.TempDir(), "nonexistent"), time.Hour)
	if removed != 0 {
		t.Fatalf("expected 0 removed from nonexistent dir, got %d", removed)
	}
}
