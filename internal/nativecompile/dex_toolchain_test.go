package nativecompile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/proc"
	"github.com/kaeawc/grit/internal/project"
)

func TestDexToolchainPrefersEnvJar(t *testing.T) {
	tmp := t.TempDir()
	jar := filepath.Join(tmp, "r8-8.6.27.jar")
	if err := os.WriteFile(jar, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("D8_R8_JAR", jar)
	t.Setenv("ANDROID_HOME", filepath.Join(tmp, "empty-sdk"))

	tc, err := loadDexToolchain(nil, nil)
	if err != nil {
		t.Fatalf("loadDexToolchain: %v", err)
	}
	if tc.Source != "env" || tc.JarPath != jar || tc.Version != "8.6.27" {
		t.Fatalf("unexpected toolchain: %#v", tc)
	}
}

func TestDexToolchainUsesNewestSDKBuildTools(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("D8_R8_JAR", "")
	t.Setenv("ANDROID_HOME", filepath.Join(tmp, "android-sdk"))

	for _, version := range []string{"35.0.0", "36.0.0"} {
		dir := filepath.Join(tmp, "android-sdk", "build-tools", version, "lib")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "d8.jar"), []byte(version), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tc, err := loadDexToolchain(nil, nil)
	if err != nil {
		t.Fatalf("loadDexToolchain: %v", err)
	}
	want := filepath.Join(tmp, "android-sdk", "build-tools", "36.0.0", "lib", "d8.jar")
	if tc.Source != "sdk-build-tools" || tc.Version != "36.0.0" || tc.JarPath != want {
		t.Fatalf("unexpected toolchain: %#v", tc)
	}
}

func TestDexToolchainResolvesR8Dependency(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("R8_VERSION", "8.6.27")
	jar := filepath.Join(tmp, "r8-8.6.27.jar")
	if err := os.WriteFile(jar, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := &fakeToolArtifactResolver{
		resolved: &m2local.Resolved{RuntimeJars: []string{jar}},
	}

	tc, err := resolveDexToolchainFromDependencies(nil, resolver)
	if err != nil {
		t.Fatalf("resolveDexToolchainFromDependencies: %v", err)
	}
	if tc.Source != "dependency" || tc.Version != "8.6.27" || tc.JarPath != jar {
		t.Fatalf("unexpected toolchain: %#v", tc)
	}
	if resolver.requested != "com.android.tools:r8:8.6.27" {
		t.Fatalf("unexpected resolver request: %s", resolver.requested)
	}
}

func TestDexToolchainResolvesCatalogVersion(t *testing.T) {
	prj := &project.Project{VersionCatalogData: map[string]string{"android-r8": "8.7.18"}}
	if got := dexToolchainDependencyVersion(prj); got != "8.7.18" {
		t.Fatalf("expected catalog r8 version, got %q", got)
	}
}

func TestDexToolchainMissingEverythingFailsClearly(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("ANDROID_HOME", filepath.Join(tmp, "empty-sdk"))
	t.Setenv("ANDROID_SDK_ROOT", filepath.Join(tmp, "empty-sdk-root"))
	t.Setenv("D8_R8_JAR", "")
	t.Setenv("R8_VERSION", "")

	_, err := loadDexToolchain(nil, nil)
	if err == nil {
		t.Fatal("expected missing toolchain error")
	}
	msg := err.Error()
	for _, want := range []string{"D8_R8_JAR", "Android SDK build-tools", "R8_VERSION"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected error to mention %s, got %v", want, err)
		}
	}
}

func TestDexToolchainRejectsMissingEnvJar(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("D8_R8_JAR", filepath.Join(tmp, "missing-r8.jar"))
	t.Setenv("ANDROID_HOME", filepath.Join(tmp, "android-sdk"))

	_, err := loadDexToolchain(nil, nil)
	if err == nil {
		t.Fatal("expected missing D8_R8_JAR error")
	}
	if !strings.Contains(err.Error(), "D8_R8_JAR") || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestD8CommandRunsJavaMain(t *testing.T) {
	tmp := t.TempDir()
	jar := filepath.Join(tmp, "r8-8.6.27.jar")
	if err := os.WriteFile(jar, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	tc := &androidDexToolchain{Version: "8.6.27", Source: "dependency", JarPath: jar}
	fake := proc.NewFake()
	fake.On(proc.MatchPrefix("java", "-cp", jar, "com.android.tools.r8.D8"), proc.Response{
		Result: proc.Result{ExitCode: 0},
	})
	restore := SwapRunner(fake)
	t.Cleanup(restore)

	if err := runD8Command(context.Background(), tc, []string{"--output", "dex", "classes.jar"}, nil, nil); err != nil {
		t.Fatalf("runD8Command: %v", err)
	}
	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected one command, got %+v", calls)
	}
	if calls[0].Name != "java" {
		t.Fatalf("expected java command, got %+v", calls[0])
	}
	for _, arg := range calls[0].Args {
		if arg == "d8" {
			t.Fatalf("D8 command should not shell out through PATH: %+v", calls[0])
		}
	}
}

func TestDexOutputsInvalidateWhenToolJarChanges(t *testing.T) {
	tmp := t.TempDir()
	jar1 := filepath.Join(tmp, "r8-8.6.27.jar")
	jar2 := filepath.Join(tmp, "r8-8.7.18.jar")
	if err := os.WriteFile(jar1, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jar2, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	classes := filepath.Join(tmp, "classes.jar")
	if err := os.WriteFile(classes, []byte("classes"), 0o644); err != nil {
		t.Fatal(err)
	}

	tc1 := &androidDexToolchain{Version: "8.6.27", Source: "dependency", JarPath: jar1}
	tc2 := &androidDexToolchain{Version: "8.7.18", Source: "dependency", JarPath: jar2}
	if sharedAppDexDir(tc1, classes, nil) == sharedAppDexDir(tc2, classes, nil) {
		t.Fatal("shared app dex cache dir should vary by toolchain")
	}
	if sharedExternalDexDir(tc1, nil) == sharedExternalDexDir(tc2, nil) {
		t.Fatal("shared external dex cache dir should vary by toolchain")
	}
}

func TestDexFreshnessIncludesToolJar(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "dex")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	dex := filepath.Join(out, "classes.dex")
	classes := filepath.Join(tmp, "classes.jar")
	tool := filepath.Join(tmp, "r8.jar")
	for _, path := range []string{dex, classes, tool} {
		if err := os.WriteFile(path, []byte(path), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	newer := time.Now()
	if err := os.Chtimes(dex, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(classes, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tool, newer, newer); err != nil {
		t.Fatal(err)
	}

	tc := &androidDexToolchain{Version: "8.6.27", Source: "dependency", JarPath: tool}
	inputs := append([]string{classes}, dexToolchainInputs(tc)...)
	if outputsNewerThanInputs(out, inputs) {
		t.Fatal("dex output should be stale when the tool jar is newer")
	}
}

func TestDexFreshnessIncludesToolchainMetadata(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "dex")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	dex := filepath.Join(out, "classes.dex")
	classes := filepath.Join(tmp, "classes.jar")
	tool := filepath.Join(tmp, "r8.jar")
	for _, path := range []string{dex, classes, tool} {
		if err := os.WriteFile(path, []byte(path), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	newer := time.Now()
	if err := os.Chtimes(classes, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tool, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dex, newer, newer); err != nil {
		t.Fatal(err)
	}

	tc1 := &androidDexToolchain{Version: "8.6.27", Source: "dependency", JarPath: tool}
	tc2 := &androidDexToolchain{Version: "8.7.18", Source: "dependency", JarPath: tool}
	if err := writeDexToolchainStamp(out, tc1); err != nil {
		t.Fatal(err)
	}
	if !dexOutputsFresh(out, []string{classes}, tc1) {
		t.Fatal("expected output to be fresh for matching toolchain metadata")
	}
	if dexOutputsFresh(out, []string{classes}, tc2) {
		t.Fatal("expected output to be stale after toolchain metadata changes")
	}
}

func TestOfflineDependencyResolutionDoesNotFallbackToCentral(t *testing.T) {
	t.Setenv("R8_VERSION", "8.6.27")
	_, err := resolveDexToolchainFromDependencies(nil, &fakeToolArtifactResolver{err: errors.New("offline: artifact not materialized")})
	if err == nil {
		t.Fatal("expected resolver error")
	}
	if !strings.Contains(err.Error(), "offline: artifact not materialized") {
		t.Fatalf("expected resolver/offline error, got %v", err)
	}
}
