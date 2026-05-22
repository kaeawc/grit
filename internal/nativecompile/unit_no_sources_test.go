package nativecompile

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/project"
)

// TestRunDebugUnitNoTestSourcesReturnsNilNotError covers the
// cert-installer-style case: a module compiled cleanly (compile.stamp
// recorded "no test sources found") but produced no UnitTest/classes
// directory. The runner used to error out with "compiled unit test
// outputs … are missing", which surfaced to users as a build failure
// even though there was nothing to run. The fix detects empty source
// roots and treats them as "no tests to run" (nil) instead.
func TestRunDebugUnitNoTestSourcesReturnsNilNotError(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "app")
	if err := os.MkdirAll(filepath.Join(moduleDir, "src", "main"), 0o755); err != nil {
		t.Fatal(err)
	}
	// No src/test/* directory — collectUnitTestSources returns empty.

	mod := &project.Module{Path: ":app", Dir: moduleDir, Type: "android-application"}
	prj := &project.Project{RootDir: root, Modules: []project.Module{*mod}}

	stdout := devNullFile(t)
	stderr := devNullFile(t)

	c := New()
	if err := c.RunDebugUnit(context.Background(), prj, ":app", "debug", stdout, stderr); err != nil {
		t.Fatalf("expected nil error for empty test sources, got %v", err)
	}
}

// TestRunDebugUnitReportsOutputsMissingWhenSourcesExist preserves the
// original error path for modules that DO have test sources but
// somehow lack compiled outputs (which is a real "missing prior step"
// problem, not an empty-tests no-op).
func TestRunDebugUnitReportsOutputsMissingWhenSourcesExist(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "app")
	testSrcDir := filepath.Join(moduleDir, "src", "test", "kotlin")
	if err := os.MkdirAll(testSrcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A test source exists; collectUnitTestSources returns non-empty.
	if err := os.WriteFile(filepath.Join(testSrcDir, "Foo.kt"), []byte("class Foo"), 0o644); err != nil {
		t.Fatal(err)
	}

	mod := &project.Module{Path: ":app", Dir: moduleDir, Type: "android-application"}
	prj := &project.Project{RootDir: root, Modules: []project.Module{*mod}}

	var stderrBuf bytes.Buffer
	stdout := devNullFile(t)
	stderrFile := devNullFile(t)

	c := New()
	err := c.RunDebugUnit(context.Background(), prj, ":app", "debug", stdout, stderrFile)
	if err == nil {
		t.Fatalf("expected outputs-missing error when test sources are present without compiled outputs")
	}
	if !strings.Contains(err.Error(), "compiled unit test outputs") {
		t.Fatalf("error should name the missing outputs, got: %v\nstderr=%q", err, stderrBuf.String())
	}
}

func devNullFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}
