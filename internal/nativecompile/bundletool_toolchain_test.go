package nativecompile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	tc, err := loadBundletoolToolchain()
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
	_, err := loadBundletoolToolchain()
	if err == nil {
		t.Fatal("expected error for missing BUNDLETOOL_JAR file")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadBundletoolFromGradleCache(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("BUNDLETOOL_JAR", "")

	// Create fake Gradle cache structure.
	jarDir := filepath.Join(tmp, ".gradle", "caches", "modules-2", "files-2.1",
		"com.android.tools.build", "bundletool", "1.15.6", "abcdef1234")
	if err := os.MkdirAll(jarDir, 0755); err != nil {
		t.Fatal(err)
	}
	jar := filepath.Join(jarDir, "bundletool-1.15.6.jar")
	if err := os.WriteFile(jar, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	tc, err := loadBundletoolToolchain()
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

func TestLoadBundletoolFromSDK(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("BUNDLETOOL_JAR", "")
	t.Setenv("ANDROID_HOME", filepath.Join(tmp, "android-sdk"))

	// Create fake SDK build-tools structure.
	btDir := filepath.Join(tmp, "android-sdk", "build-tools", "36.0.0", "lib")
	if err := os.MkdirAll(btDir, 0755); err != nil {
		t.Fatal(err)
	}
	jar := filepath.Join(btDir, "bundletool.jar")
	if err := os.WriteFile(jar, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	tc, err := loadBundletoolToolchain()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tc.JarPath != jar {
		t.Fatalf("expected jar path %s, got %s", jar, tc.JarPath)
	}
}

func TestLoadBundletoolNotFound(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("BUNDLETOOL_JAR", "")
	t.Setenv("ANDROID_HOME", filepath.Join(tmp, "empty-sdk"))

	_, err := loadBundletoolToolchain()
	if err == nil {
		t.Fatal("expected error when bundletool is not found anywhere")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}
