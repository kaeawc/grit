package nativecompile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
)

func TestBundletoolValidateRejectsNil(t *testing.T) {
	var tc *bundletoolToolchain
	if err := tc.validate(); err == nil {
		t.Fatal("expected error for nil toolchain")
	}
}

func TestBundletoolValidateRejectsEmptyPath(t *testing.T) {
	tc := &bundletoolToolchain{Version: "1.15.6", JarPath: ""}
	if err := tc.validate(); err == nil {
		t.Fatal("expected error for empty jar path")
	}
}

func TestBundletoolValidateRejectsMissingFile(t *testing.T) {
	tc := &bundletoolToolchain{Version: "1.15.6", JarPath: "/nonexistent/bundletool.jar"}
	err := tc.validate()
	if err == nil {
		t.Fatal("expected error for missing jar")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBundletoolValidateAcceptsExistingFile(t *testing.T) {
	tmp := t.TempDir()
	jar := filepath.Join(tmp, "bundletool.jar")
	if err := os.WriteFile(jar, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	tc := &bundletoolToolchain{Version: "1.15.6", JarPath: jar}
	if err := tc.validate(); err != nil {
		t.Fatalf("expected valid toolchain, got %v", err)
	}
}

func TestBundletoolVersionFromPath(t *testing.T) {
	tests := []struct {
		path    string
		version string
	}{
		{"bundletool-all-1.15.6.jar", "1.15.6"},
		{"bundletool-1.14.0.jar", "1.14.0"},
		{"bundletool.jar", "unknown"},
	}
	for _, tt := range tests {
		got := bundletoolVersionFromPath(tt.path)
		if got != tt.version {
			t.Errorf("bundletoolVersionFromPath(%q) = %q, want %q", tt.path, got, tt.version)
		}
	}
}

func TestLoadBundletoolFromEnvVar(t *testing.T) {
	tmp := t.TempDir()
	jar := filepath.Join(tmp, "bundletool-all-1.15.6.jar")
	if err := os.WriteFile(jar, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BUNDLETOOL_JAR", jar)
	tc, err := loadBundletoolToolchain(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tc.JarPath != jar {
		t.Fatalf("expected jar path %s, got %s", jar, tc.JarPath)
	}
	if tc.Version != "1.15.6" {
		t.Fatalf("expected version 1.15.6, got %s", tc.Version)
	}
}

func TestLoadBundletoolEnvVarMissingFile(t *testing.T) {
	t.Setenv("BUNDLETOOL_JAR", "/nonexistent/bundletool.jar")
	_, err := loadBundletoolToolchain(nil, nil)
	if err == nil {
		t.Fatal("expected error for missing BUNDLETOOL_JAR file")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadBundletoolFromSDK(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("BUNDLETOOL_JAR", "")
	t.Setenv("ANDROID_HOME", filepath.Join(tmp, "android-sdk"))

	btDir := filepath.Join(tmp, "android-sdk", "build-tools", "36.0.0", "lib")
	if err := os.MkdirAll(btDir, 0755); err != nil {
		t.Fatal(err)
	}
	jar := filepath.Join(btDir, "bundletool.jar")
	if err := os.WriteFile(jar, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	tc, err := loadBundletoolToolchain(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tc.JarPath != jar {
		t.Fatalf("expected jar path %s, got %s", jar, tc.JarPath)
	}
}

func TestResolveBundletoolFromDependencies(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("BUNDLETOOL_VERSION", "1.15.6")
	jar := filepath.Join(tmp, "bundletool-all-1.15.6.jar")
	if err := os.WriteFile(jar, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	resolver := &fakeToolArtifactResolver{
		resolved: &m2local.Resolved{RuntimeJars: []string{jar}},
	}

	tc, err := resolveBundletoolFromDependencies(resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tc.JarPath != jar {
		t.Fatalf("expected jar path %s, got %s", jar, tc.JarPath)
	}
	if tc.Version != "1.15.6" {
		t.Fatalf("expected version 1.15.6, got %s", tc.Version)
	}
	if resolver.requested != "com.android.tools.build:bundletool:1.15.6" {
		t.Fatalf("unexpected resolver request: %s", resolver.requested)
	}
}

func TestResolveBundletoolRequiresConfiguredVersion(t *testing.T) {
	t.Setenv("BUNDLETOOL_VERSION", "")
	_, err := resolveBundletoolFromDependencies(&fakeToolArtifactResolver{})
	if err == nil {
		t.Fatal("expected error when bundletool version is not configured")
	}
	if !strings.Contains(err.Error(), "version not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveMavenToolJarUsesCompileAndRuntimeJars(t *testing.T) {
	tmp := t.TempDir()
	jarPath := filepath.Join(tmp, "bundletool-all-1.15.6.jar")
	if err := os.WriteFile(jarPath, []byte("fake-jar"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveMavenToolJar(&fakeToolArtifactResolver{
		resolved: &m2local.Resolved{
			CompileJars: []string{filepath.Join(tmp, "dep.jar")},
			RuntimeJars: []string{jarPath},
		},
	}, mavenToolArtifact{
		Group:        "com.android.tools.build",
		Artifact:     "bundletool",
		Version:      "1.15.6",
		JarBaseNames: []string{"bundletool-all-"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != jarPath {
		t.Fatalf("expected %s, got %s", jarPath, got)
	}
}

func TestResolveMavenToolJarReportsMissingJar(t *testing.T) {
	_, err := resolveMavenToolJar(&fakeToolArtifactResolver{
		resolved: &m2local.Resolved{RuntimeJars: []string{"/missing/bundletool-all-1.15.6.jar"}},
	}, mavenToolArtifact{
		Group:        "com.android.tools.build",
		Artifact:     "bundletool",
		Version:      "1.15.6",
		JarBaseNames: []string{"bundletool-all-"},
	})
	if err == nil {
		t.Fatal("expected error for missing resolved jar")
	}
	if !strings.Contains(err.Error(), "artifact jar not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveMavenToolJarWrapsResolverError(t *testing.T) {
	_, err := resolveMavenToolJar(&fakeToolArtifactResolver{
		err: errors.New("resolver failed"),
	}, mavenToolArtifact{Group: "g", Artifact: "a", Version: "1"})
	if err == nil {
		t.Fatal("expected resolver error")
	}
	if !strings.Contains(err.Error(), "resolve g:a:1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type fakeToolArtifactResolver struct {
	resolved  *m2local.Resolved
	err       error
	requested string
}

func (f *fakeToolArtifactResolver) Resolve(deps *modulebuild.Dependencies) (*m2local.Resolved, error) {
	if len(deps.Main) > 0 {
		f.requested = deps.Main[0].Value
	}
	return f.resolved, f.err
}
