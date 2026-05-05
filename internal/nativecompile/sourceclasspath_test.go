package nativecompile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/testutil"
)

func TestCollectSourcesMissingDirectory(t *testing.T) {
	t.Parallel()

	got, err := collectSources(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("collect sources: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no sources, got %v", got)
	}
}

func TestDiscoverJUnitTestsMissingDirectory(t *testing.T) {
	t.Parallel()

	got, err := discoverJUnitTests(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("discover junit tests: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no tests, got %v", got)
	}
}

func TestDiscoverJUnitTestsFindsQualifiedNames(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testutil.WriteFile(t, dir, "src/test/kotlin/com/example/WidgetTest.kt", "package com.example\nclass WidgetTest\nclass Helper\nclass OtherTest\n")

	got, err := discoverJUnitTests(filepath.Join(dir, "src", "test"))
	if err != nil {
		t.Fatalf("discover junit tests: %v", err)
	}
	want := []string{"com.example.OtherTest", "com.example.WidgetTest"}
	if len(got) != len(want) {
		t.Fatalf("unexpected test count: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected tests: got %v want %v", got, want)
		}
	}
}

func TestArtifactFamilyKeyNormalizesAndroidXActivityKtx(t *testing.T) {
	t.Parallel()

	if got := artifactFamilyKey("androidx.activity", "activity-ktx"); got != "androidx.activity:activity" {
		t.Fatalf("unexpected family key: %s", got)
	}
}

func TestArtifactFamilyKeyNormalizesLifecycleLiveDataCoreKtx(t *testing.T) {
	t.Parallel()

	if got := artifactFamilyKey("androidx.lifecycle", "lifecycle-livedata-core-ktx"); got != "androidx.lifecycle:lifecycle-livedata-core" {
		t.Fatalf("unexpected lifecycle family key: %s", got)
	}
}

func TestArtifactFamilyKeyNormalizesComposeDesktopAndJvmStubs(t *testing.T) {
	t.Parallel()

	if got := artifactFamilyKey("androidx.compose.animation", "animation-core-desktop"); got != "androidx.compose.animation:animation-core" {
		t.Fatalf("unexpected compose desktop family key: %s", got)
	}
	if got := artifactFamilyKey("androidx.compose.animation", "animation-core-jvmstubs"); got != "androidx.compose.animation:animation-core" {
		t.Fatalf("unexpected compose jvmstubs family key: %s", got)
	}
}

func TestArtifactFamilyKeyKeepsGuavaListenableFutureDistinct(t *testing.T) {
	t.Parallel()

	if got := artifactFamilyKey("com.google.guava", "listenablefuture"); got != "com.google.guava:listenablefuture" {
		t.Fatalf("unexpected guava family key: %s", got)
	}
}

func TestArtifactFamilyKeyNormalizesOkioJvm(t *testing.T) {
	t.Parallel()

	if got := artifactFamilyKey("com.squareup.okio", "okio-jvm"); got != "com.squareup.okio:okio" {
		t.Fatalf("unexpected okio family key: %s", got)
	}
}

func TestArtifactFamilyKeyNormalizesCoroutinesJvm(t *testing.T) {
	t.Parallel()

	if got := artifactFamilyKey("org.jetbrains.kotlinx", "kotlinx-coroutines-core-jvm"); got != "org.jetbrains.kotlinx:kotlinx-coroutines-core" {
		t.Fatalf("unexpected coroutines family key: %s", got)
	}
}

func TestArtifactFamilyKeyNormalizesActivationImplementations(t *testing.T) {
	t.Parallel()

	if got := artifactFamilyKey("com.sun.mail", "android-activation"); got != "javax.activation:activation" {
		t.Fatalf("unexpected android activation family key: %s", got)
	}
	if got := artifactFamilyKey("jakarta.activation", "jakarta.activation-api"); got != "javax.activation:activation" {
		t.Fatalf("unexpected jakarta activation family key: %s", got)
	}
}

func TestArtifactFamilyKeyNormalizesLegacyAsmGroup(t *testing.T) {
	t.Parallel()

	if got := artifactFamilyKey("asm", "asm-analysis"); got != "org.ow2.asm:asm-analysis" {
		t.Fatalf("unexpected asm family key: %s", got)
	}
}

func TestFilterEmptyListenableFutureDropsPlaceholderWhenGuavaPresent(t *testing.T) {
	t.Parallel()

	paths := []string{
		"/tmp/x/com.google.guava/guava/33.3.1-android/guava-33.3.1-android.jar",
		"/tmp/x/com.google.guava/listenablefuture/9999.0-empty-to-avoid-conflict-with-guava/listenablefuture-9999.0-empty-to-avoid-conflict-with-guava.jar",
	}
	got := filterEmptyListenableFuture(paths)
	if len(got) != 1 || got[0] != paths[0] {
		t.Fatalf("unexpected filtered paths: %#v", got)
	}
}

func TestFilterEmptyListenableFutureDropsStandardJarWhenGuavaPresent(t *testing.T) {
	t.Parallel()

	paths := []string{
		"/tmp/x/com.google.guava/guava/31.0.1-jre/guava-31.0.1-jre.jar",
		"/tmp/x/com.google.guava/listenablefuture/1.0/listenablefuture-1.0.jar",
	}
	got := filterEmptyListenableFuture(paths)
	if len(got) != 1 || got[0] != paths[0] {
		t.Fatalf("unexpected filtered paths: %#v", got)
	}
}

func TestFilterEmptyListenableFutureRecognizesExpandedGroupPath(t *testing.T) {
	t.Parallel()

	paths := []string{
		"/tmp/x/com/google/guava/guava/33.3.1-jre/guava-33.3.1-jre.jar",
		"/tmp/x/com/google/guava/listenablefuture/9999.0-empty-to-avoid-conflict-with-guava/listenablefuture-9999.0-empty-to-avoid-conflict-with-guava.jar",
	}
	got := filterEmptyListenableFuture(paths)
	if len(got) != 1 || got[0] != paths[0] {
		t.Fatalf("unexpected filtered paths: %#v", got)
	}
}

func TestFilterKnownRuntimeDuplicatesDropsLiveDataCoreKtxWhenCorePresent(t *testing.T) {
	t.Parallel()

	paths := []string{
		"/tmp/.grit/aar/androidx.lifecycle/lifecycle-livedata-core-ktx/2.3.1/classes.jar",
		"/tmp/.grit/aar/androidx.lifecycle/lifecycle-livedata-core/2.10.0/classes.jar",
	}
	got := filterKnownRuntimeDuplicates(paths)
	if len(got) != 1 || got[0] != paths[1] {
		t.Fatalf("unexpected filtered paths: %#v", got)
	}
}

func TestArtifactKeyVersionRecognizesMaterializedMavenLayout(t *testing.T) {
	t.Parallel()

	key, version, ok := artifactKeyVersion("/tmp/repo/.grit/worktree/materialized-m2/androidx/lifecycle/lifecycle-livedata-core/2.10.0/lifecycle-livedata-core-2.10.0.jar")
	if !ok {
		t.Fatal("expected materialized dependency path to parse")
	}
	if key != "androidx.lifecycle:lifecycle-livedata-core" || version != "2.10.0" {
		t.Fatalf("unexpected parsed artifact identity: %s %s", key, version)
	}
}

func TestDiscoverJUnitTestsCachedWritesFilteredCache(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "src", "test")
	testutil.WriteFile(t, filepath.Dir(filepath.Dir(root)), "src/test/kotlin/com/example/WidgetTest.kt", "package com.example\nclass WidgetTest\n")
	testutil.WriteFile(t, filepath.Dir(filepath.Dir(root)), "src/test/kotlin/com/example/automobiletest/AutoMobileTest.kt", "package com.example.automobiletest\nclass AutoMobileTest\n")

	got, err := discoverJUnitTestsCached(root, true)
	if err != nil {
		t.Fatalf("discover cached tests: %v", err)
	}
	want := []string{"com.example.WidgetTest"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("unexpected discovered tests: got %v want %v", got, want)
	}

	cachePath := junitDiscoveryCachePath(root, true)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var cached []string
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatalf("unmarshal cache: %v", err)
	}
	if len(cached) != len(want) || cached[0] != want[0] {
		t.Fatalf("unexpected cached tests: got %v want %v", cached, want)
	}
}
