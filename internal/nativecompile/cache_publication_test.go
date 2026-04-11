package nativecompile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/testutil"
)

func TestSharedCompileCacheRoundTrip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	classesDir := filepath.Join(root, "classes")
	cacheDir := filepath.Join(root, "cache")
	moduleJar := filepath.Join(root, "module-classes.jar")
	testutil.WriteFile(t, classesDir, "com/example/App.class", "bytecode")
	testutil.WriteFile(t, root, "compile.stamp", "fingerprint")
	testutil.WriteFile(t, root, "module-classes.jar", "jar-bytes")
	if err := saveSharedCompileCache(classesDir, moduleJar, cacheDir); err != nil {
		t.Fatalf("save shared compile cache: %v", err)
	}

	restoredClasses := filepath.Join(root, "restored-classes")
	restoredJar := filepath.Join(root, "restored-module-classes.jar")
	if !restoreSharedCompileCache(restoredClasses, restoredJar, cacheDir) {
		t.Fatal("expected shared compile cache to restore")
	}

	if got, err := os.ReadFile(filepath.Join(restoredClasses, "com/example/App.class")); err != nil || string(got) != "bytecode" {
		t.Fatalf("restored classes mismatch: %q %v", got, err)
	}
	if got, err := os.ReadFile(restoredJar); err != nil || string(got) != "jar-bytes" {
		t.Fatalf("restored jar mismatch: %q %v", got, err)
	}
}
