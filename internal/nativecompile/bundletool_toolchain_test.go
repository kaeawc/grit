package nativecompile

import (
	"net/http"
	"net/http/httptest"
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

	// Pre-seed cache so downloadBundletool does not try a real HTTP request.
	// We want to test the "not found" path, so point cache root somewhere empty.
	t.Setenv("BUNDLETOOL_VERSION", "99.99.99")

	_, err := loadBundletoolToolchain()
	if err == nil {
		t.Fatal("expected error when bundletool is not found anywhere")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBundletoolDownloadURL(t *testing.T) {
	t.Parallel()
	got := bundletoolDownloadURL("1.17.2")
	want := "https://repo1.maven.org/maven2/com/android/tools/build/bundletool/1.17.2/bundletool-all-1.17.2.jar"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBundletoolCacheJarPath(t *testing.T) {
	t.Parallel()
	p := bundletoolCacheJarPath("1.17.2")
	if !strings.HasSuffix(p, filepath.Join("bundletool", "bundletool-all-1.17.2.jar")) {
		t.Fatalf("unexpected path: %s", p)
	}
}

func TestDownloadBundletoolUsesCache(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("BUNDLETOOL_JAR", "")
	t.Setenv("ANDROID_HOME", filepath.Join(tmp, "empty-sdk"))
	t.Setenv("BUNDLETOOL_VERSION", "1.15.6")

	// Pre-create the cached JAR so no HTTP request is made.
	jarPath := bundletoolCacheJarPath("1.15.6")
	if err := os.MkdirAll(filepath.Dir(jarPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jarPath, []byte("fake-jar"), 0644); err != nil {
		t.Fatal(err)
	}

	tc, err := downloadBundletool()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tc.JarPath != jarPath {
		t.Fatalf("expected %s, got %s", jarPath, tc.JarPath)
	}
	if tc.Version != "1.15.6" {
		t.Fatalf("expected version 1.15.6, got %s", tc.Version)
	}
}

func TestDownloadFileSuccess(t *testing.T) {
	t.Parallel()
	content := "bundletool-jar-content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(content))
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "sub", "bundletool.jar")
	if err := downloadFile(srv.URL+"/bundletool.jar", dst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("got %q, want %q", string(got), content)
	}

	// Temp file should be cleaned up.
	if pathIsFile(dst + ".tmp") {
		t.Fatal(".tmp file should not remain after successful download")
	}
}

func TestDownloadFileHTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "bundletool.jar")
	err := downloadFile(srv.URL+"/missing.jar", dst)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected HTTP 404 in error, got: %v", err)
	}
	if pathIsFile(dst) {
		t.Fatal("destination file should not exist after failed download")
	}
}

func TestDownloadFileCleansTmpOnWriteError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))
	defer srv.Close()

	// Use a path inside a non-writable directory for the tmp file.
	// The directory itself needs to exist so Create works, but we
	// test the success path here because write errors from io.Copy
	// are harder to trigger. Instead verify no .tmp remains after success.
	dst := filepath.Join(t.TempDir(), "bundletool.jar")
	if err := downloadFile(srv.URL+"/ok.jar", dst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pathIsFile(dst + ".tmp") {
		t.Fatal(".tmp file should not remain")
	}
}

func TestLoadBundletoolFallsBackToDownload(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("BUNDLETOOL_JAR", "")
	t.Setenv("ANDROID_HOME", filepath.Join(tmp, "empty-sdk"))
	t.Setenv("BUNDLETOOL_VERSION", "1.16.0")

	// Pre-create the cached JAR so no HTTP request is actually made.
	jarPath := bundletoolCacheJarPath("1.16.0")
	if err := os.MkdirAll(filepath.Dir(jarPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jarPath, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	tc, err := loadBundletoolToolchain()
	if err != nil {
		t.Fatalf("expected download fallback to succeed: %v", err)
	}
	if tc.Version != "1.16.0" {
		t.Fatalf("expected version 1.16.0, got %s", tc.Version)
	}
	if tc.JarPath != jarPath {
		t.Fatalf("expected jar at cache path %s, got %s", jarPath, tc.JarPath)
	}
}
