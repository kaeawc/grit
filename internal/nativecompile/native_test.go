package nativecompile

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/testutil"
)

func TestCopyFilePreservesModTime(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := testutil.WriteFile(t, dir, "src.txt", "hello")
	dst := filepath.Join(dir, "dst.txt")
	modTime := time.Unix(1_700_000_000, 0)
	testutil.Touch(t, src, modTime)

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copy file: %v", err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dst: %v", err)
	}
	if !info.ModTime().Equal(modTime) {
		t.Fatalf("modtime mismatch: got %v want %v", info.ModTime(), modTime)
	}
}

func TestStampRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "compile.stamp")
	if stampMatches(path, "abc") {
		t.Fatal("missing stamp unexpectedly matched")
	}
	if err := writeStamp(path, "abc"); err != nil {
		t.Fatalf("write stamp: %v", err)
	}
	if !stampMatches(path, "abc") {
		t.Fatal("written stamp did not match")
	}
	if stampMatches(path, "xyz") {
		t.Fatal("stamp matched wrong value")
	}
}

func TestHasOutputFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if hasOutputFiles(dir) {
		t.Fatal("empty dir should not report outputs")
	}
	testutil.WriteFile(t, dir, "classes.bin", "data")
	if !hasOutputFiles(dir) {
		t.Fatal("dir with file should report outputs")
	}
}

func TestCompileStateAddProjectDepsRejectsCycle(t *testing.T) {
	t.Parallel()

	state := newCompileState()
	if err := state.addProjectDeps(":app#debug", []string{":lib#debug"}); err != nil {
		t.Fatalf("add deps: %v", err)
	}
	if err := state.addProjectDeps(":lib#debug", []string{":core#debug"}); err != nil {
		t.Fatalf("add deps: %v", err)
	}
	if err := state.addProjectDeps(":core#debug", []string{":app#debug"}); err == nil {
		t.Fatal("expected cycle detection error")
	}
}

func TestCompileStateAddProjectDepsAllowsAcyclicGraph(t *testing.T) {
	t.Parallel()

	state := newCompileState()
	if err := state.addProjectDeps(":app#debug", []string{":lib#debug", ":ui#debug"}); err != nil {
		t.Fatalf("add deps: %v", err)
	}
	if err := state.addProjectDeps(":lib#debug", []string{":core#debug"}); err != nil {
		t.Fatalf("add deps: %v", err)
	}
	if err := state.addProjectDeps(":ui#debug", []string{":core#debug"}); err != nil {
		t.Fatalf("add deps: %v", err)
	}
}

func TestKotlinTestShimJarBuilds(t *testing.T) {
	t.Parallel()

	jar, err := buildKotlinTestShimJar()
	if err != nil {
		t.Skipf("build kotlin test shim jar: %v", err)
	}
	if jar == "" {
		t.Fatal("expected kotlin test shim jar to be built")
	}
	info, err := os.Stat(jar)
	if err != nil {
		t.Fatalf("stat shim jar: %v", err)
	}
	file, err := os.Open(jar)
	if err != nil {
		t.Fatalf("open shim jar: %v", err)
	}
	defer func() { _ = file.Close() }()
	zr, err := zip.NewReader(file, info.Size())
	if err != nil {
		t.Fatalf("read shim jar: %v", err)
	}
	found := false
	for _, f := range zr.File {
		if f.Name == "kotlin/test/Test.class" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("shim jar missing kotlin/test/Test.class: %s", jar)
	}
}

func TestJUnitPlatformRunnerJarBuilds(t *testing.T) {
	t.Parallel()

	jar, err := buildJUnitPlatformRunnerJar()
	if err != nil {
		t.Skipf("build junit platform runner jar: %v", err)
	}
	if jar == "" {
		t.Fatal("expected junit platform runner jar to be built")
	}
	info, err := os.Stat(jar)
	if err != nil {
		t.Fatalf("stat runner jar: %v", err)
	}
	file, err := os.Open(jar)
	if err != nil {
		t.Fatalf("open runner jar: %v", err)
	}
	defer func() { _ = file.Close() }()
	zr, err := zip.NewReader(file, info.Size())
	if err != nil {
		t.Fatalf("read runner jar: %v", err)
	}
	found := false
	for _, f := range zr.File {
		if f.Name == "grit/junit/PlatformRunner.class" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("runner jar missing grit/junit/PlatformRunner.class: %s", jar)
	}
}
