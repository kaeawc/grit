package nativecompile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
