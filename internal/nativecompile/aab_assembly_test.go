package nativecompile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundletoolBuildBundleArgs(t *testing.T) {
	args := bundletoolBuildBundleArgs(
		"/sdk/bundletool.jar",
		[]string{"base.zip"},
		"app.aab",
		"",
	)
	want := []string{
		"-jar", "/sdk/bundletool.jar",
		"build-bundle",
		"--modules=base.zip",
		"--output=app.aab",
	}
	if len(args) != len(want) {
		t.Fatalf("got %d args, want %d: %v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestBundletoolBuildBundleArgsWithConfig(t *testing.T) {
	args := bundletoolBuildBundleArgs(
		"/sdk/bundletool.jar",
		[]string{"base.zip", "feature.zip"},
		"app.aab",
		"BundleConfig.pb",
	)
	// Verify modules are comma-joined.
	found := false
	for _, a := range args {
		if a == "--modules=base.zip,feature.zip" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected comma-joined --modules flag, got %v", args)
	}
	// Verify config flag is present.
	hasConfig := false
	for _, a := range args {
		if a == "--config=BundleConfig.pb" {
			hasConfig = true
		}
	}
	if !hasConfig {
		t.Errorf("expected --config flag, got %v", args)
	}
}

func TestRunBundletoolBuildBundleRejectsNilToolchain(t *testing.T) {
	err := runBundletoolBuildBundle(
		t.Context(),
		nil,
		[]string{"base.zip"},
		"app.aab",
		"",
		os.Stdout, os.Stderr,
	)
	if err == nil {
		t.Fatal("expected error for nil toolchain")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunBundletoolBuildBundleRejectsEmptyModules(t *testing.T) {
	tmp := t.TempDir()
	jar := filepath.Join(tmp, "bundletool.jar")
	if err := os.WriteFile(jar, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	tc := &bundletoolToolchain{Version: "1.15.6", JarPath: jar}
	err := runBundletoolBuildBundle(
		t.Context(),
		tc,
		nil,
		"app.aab",
		"",
		os.Stdout, os.Stderr,
	)
	if err == nil {
		t.Fatal("expected error for empty module zips")
	}
	if !strings.Contains(err.Error(), "no module zips") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunBundletoolBuildBundleRejectsMissingModuleZip(t *testing.T) {
	tmp := t.TempDir()
	jar := filepath.Join(tmp, "bundletool.jar")
	if err := os.WriteFile(jar, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	tc := &bundletoolToolchain{Version: "1.15.6", JarPath: jar}
	err := runBundletoolBuildBundle(
		t.Context(),
		tc,
		[]string{"/nonexistent/base.zip"},
		"app.aab",
		"",
		os.Stdout, os.Stderr,
	)
	if err == nil {
		t.Fatal("expected error for missing module zip")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBundletoolBuildBundleArgsMultipleModules(t *testing.T) {
	t.Parallel()
	args := bundletoolBuildBundleArgs(
		"/sdk/bundletool.jar",
		[]string{"base.zip", "feature1.zip", "feature2.zip"},
		"app.aab",
		"BundleConfig.pb",
	)
	// Verify modules are comma-joined with all three.
	foundModules := false
	foundConfig := false
	for _, a := range args {
		if a == "--modules=base.zip,feature1.zip,feature2.zip" {
			foundModules = true
		}
		if a == "--config=BundleConfig.pb" {
			foundConfig = true
		}
	}
	if !foundModules {
		t.Errorf("expected all three modules comma-joined, got %v", args)
	}
	if !foundConfig {
		t.Errorf("expected --config flag, got %v", args)
	}
}

func TestBundletoolBuildBundleArgsSingleModuleNoConfig(t *testing.T) {
	t.Parallel()
	args := bundletoolBuildBundleArgs(
		"/sdk/bundletool.jar",
		[]string{"base.zip"},
		"output.aab",
		"",
	)
	for _, a := range args {
		if strings.HasPrefix(a, "--config=") {
			t.Errorf("config flag should not appear when bundleConfigPath is empty, got %v", args)
		}
	}
	if len(args) != 5 {
		t.Errorf("expected 5 args (jar, build-bundle, modules, output), got %d: %v", len(args), args)
	}
}
