package m2local

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file is a regression suite for the resolver's behavior on the
// real-world KMP / Android library shapes that surfaced as fragile while
// getting the canonical Google reference Android codebase to build
// end-to-end. Each test sets up a synthetic Gradle Module Metadata
// (`.module`) graph in the resolver's CacheRoot, then calls
// `resolveClosure` and asserts which artifact gets selected.
//
// The shapes covered:
//
//   - KMP umbrella → JVM variant via availableAt (kotlinx-coroutines-core
//     1.9.0 case).
//   - Multi-platform KMP umbrella → JVM beats native/common/js.
//   - Android library published as .aar directly at the umbrella, no
//     availableAt redirect (coil-compose 2.7.0 case).
//   - Umbrella that only has -android (no -jvm) — resolver follows the
//     -android availableAt.
//
// These tests don't depend on the network or on a real catalog; they
// exercise the moduleVariant selection + availableAt redirect logic
// directly through resolveClosure.

func TestKMPVariantUmbrellaRedirectsToJVM(t *testing.T) {
	t.Parallel()
	resolver := New(t.TempDir(), t.TempDir(), nil, nil)
	stageKMPUmbrellaWithJVMVariant(t, resolver, "org.example", "kotlinx-coroutines-core", "1.9.0")

	jars, _, err := resolver.resolveClosure([]Coordinate{
		{Group: "org.example", Module: "kotlinx-coroutines-core", Version: "1.9.0"},
	})
	if err != nil {
		t.Fatalf("resolveClosure: %v", err)
	}
	if len(jars) != 1 {
		t.Fatalf("expected one jar, got %v", jars)
	}
	if !strings.Contains(jars[0], "/kotlinx-coroutines-core-jvm/1.9.0/") || !strings.HasSuffix(jars[0], "kotlinx-coroutines-core-jvm-1.9.0.jar") {
		t.Fatalf("expected -jvm variant jar (via availableAt), got %q", jars[0])
	}
}

func TestKMPVariantMultiPlatformPicksJVM(t *testing.T) {
	t.Parallel()
	resolver := New(t.TempDir(), t.TempDir(), nil, nil)

	// Stage an umbrella with native + js + jvm variants, each redirecting
	// to a platform-specific module. Only the -jvm variant has a jar
	// staged in the cache — that's deliberately the one the resolver
	// should follow.
	root := Coordinate{Group: "org.example", Module: "atomicfu", Version: "0.21.0"}
	rootBase := resolver.moduleBasePath(root) + "/hash"
	if err := os.MkdirAll(rootBase, 0o755); err != nil {
		t.Fatal(err)
	}
	rootBody := `{
		"variants": [
			{
				"name": "iosArm64ApiElements-published",
				"attributes": {
					"org.jetbrains.kotlin.platform.type": "native",
					"org.gradle.usage": "kotlin-api"
				},
				"available-at": {"group":"org.example","module":"atomicfu-iosarm64","version":"0.21.0"}
			},
			{
				"name": "jsApiElements-published",
				"attributes": {
					"org.jetbrains.kotlin.platform.type": "js",
					"org.gradle.usage": "kotlin-api"
				},
				"available-at": {"group":"org.example","module":"atomicfu-js","version":"0.21.0"}
			},
			{
				"name": "metadataApiElements",
				"attributes": {
					"org.jetbrains.kotlin.platform.type": "common",
					"org.gradle.usage": "kotlin-metadata"
				},
				"files": [{"name":"atomicfu-0.21.0.jar","url":"atomicfu-0.21.0.jar"}]
			},
			{
				"name": "jvmRuntimeElements-published",
				"attributes": {
					"org.jetbrains.kotlin.platform.type": "jvm",
					"org.gradle.usage": "java-runtime"
				},
				"available-at": {"group":"org.example","module":"atomicfu-jvm","version":"0.21.0"}
			}
		]
	}`
	writeModuleAt(t, rootBase, root.Module, root.Version, rootBody)
	stageJVMVariantArtifact(t, resolver, "org.example", "atomicfu-jvm", "0.21.0")

	jars, _, err := resolver.resolveClosure([]Coordinate{root})
	if err != nil {
		t.Fatalf("resolveClosure: %v", err)
	}
	if len(jars) != 1 {
		t.Fatalf("expected one jar, got %v", jars)
	}
	if !strings.Contains(jars[0], "/atomicfu-jvm/0.21.0/") || !strings.HasSuffix(jars[0], "atomicfu-jvm-0.21.0.jar") {
		t.Fatalf("expected -jvm variant jar (not native/js/common metadata), got %q", jars[0])
	}
}

func TestAndroidUmbrellaWithoutAvailableAtPicksUmbrellaAar(t *testing.T) {
	t.Parallel()
	// coordinateForArtifact requires the `files-2.1` marker in cache
	// paths to recover the coord from the aar's filesystem location;
	// emulate that segment in this resolver's cache root.
	cacheRoot := filepath.Join(t.TempDir(), "files-2.1")
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	resolver := New(cacheRoot, t.TempDir(), nil, nil)

	root := Coordinate{Group: "io.example", Module: "coil-compose", Version: "2.7.0"}
	base := resolver.moduleBasePath(root) + "/hash"
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
		"variants": [
			{
				"name": "releaseVariantReleaseApiPublication",
				"attributes": {
					"org.gradle.category": "library",
					"org.gradle.libraryelements": "aar",
					"org.gradle.usage": "java-api"
				},
				"files": [{"name":"coil-compose-2.7.0.aar","url":"coil-compose-2.7.0.aar"}]
			},
			{
				"name": "releaseVariantReleaseRuntimePublication",
				"attributes": {
					"org.gradle.category": "library",
					"org.gradle.libraryelements": "aar",
					"org.gradle.usage": "java-runtime"
				},
				"files": [{"name":"coil-compose-2.7.0.aar","url":"coil-compose-2.7.0.aar"}]
			},
			{
				"name": "releaseVariantReleaseSourcePublication",
				"attributes": {
					"org.gradle.category": "documentation",
					"org.gradle.usage": "java-runtime"
				},
				"files": [{"name":"coil-compose-2.7.0-sources.jar","url":"coil-compose-2.7.0-sources.jar"}]
			}
		]
	}`
	writeModuleAt(t, base, root.Module, root.Version, body)
	if err := os.WriteFile(filepath.Join(base, "coil-compose-2.7.0.aar"), minimalAAR(t), 0o644); err != nil {
		t.Fatal(err)
	}

	_, libs, err := resolver.resolveClosure([]Coordinate{root})
	if err != nil {
		t.Fatalf("resolveClosure: %v", err)
	}
	if len(libs) != 1 {
		t.Fatalf("expected one AndroidLibrary (umbrella .aar), got %#v", libs)
	}
	// The .aar got picked up and aar-extraction produced an
	// AndroidLibrary tied back to the umbrella coord. ID encodes the
	// coordinate; ManifestPath/ResDir point at extracted content. We
	// don't pin exact extracted paths here — just that the umbrella's
	// aar made it through and produced an Android library entry rather
	// than falling through to a -jvm sibling or returning nothing.
	if !strings.Contains(libs[0].ID, "io.example") || !strings.Contains(libs[0].ID, "coil-compose") {
		t.Fatalf("AndroidLibrary.ID should reference the umbrella coord, got %+v", libs[0])
	}
}

func TestKMPVariantUmbrellaRedirectsToAndroid(t *testing.T) {
	t.Parallel()
	resolver := New(t.TempDir(), t.TempDir(), nil, nil)

	// Stage an umbrella that only publishes an Android variant — no JVM
	// option. The resolver should follow availableAt to the -android
	// sibling rather than failing or falling through to common metadata.
	root := Coordinate{Group: "androidx.example", Module: "collection", Version: "1.4.0"}
	rootBase := resolver.moduleBasePath(root) + "/hash"
	if err := os.MkdirAll(rootBase, 0o755); err != nil {
		t.Fatal(err)
	}
	rootBody := `{
		"variants": [
			{
				"name": "metadataApiElements",
				"attributes": {
					"org.jetbrains.kotlin.platform.type": "common",
					"org.gradle.usage": "kotlin-metadata"
				},
				"files": [{"name":"collection-metadata-1.4.0.jar","url":"collection-metadata-1.4.0.jar"}]
			},
			{
				"name": "androidJvmReleaseRuntimeElements",
				"attributes": {
					"org.jetbrains.kotlin.platform.type": "androidJvm",
					"org.gradle.usage": "java-runtime"
				},
				"available-at": {"group":"androidx.example","module":"collection-android","version":"1.4.0"}
			}
		]
	}`
	writeModuleAt(t, rootBase, root.Module, root.Version, rootBody)
	stageJVMVariantArtifact(t, resolver, "androidx.example", "collection-android", "1.4.0")

	jars, _, err := resolver.resolveClosure([]Coordinate{root})
	if err != nil {
		t.Fatalf("resolveClosure: %v", err)
	}
	if len(jars) != 1 {
		t.Fatalf("expected one jar (-android variant), got %v", jars)
	}
	if !strings.Contains(jars[0], "/collection-android/1.4.0/") || !strings.HasSuffix(jars[0], "collection-android-1.4.0.jar") {
		t.Fatalf("expected -android variant jar (followed via availableAt), got %q", jars[0])
	}
}

func stageKMPUmbrellaWithJVMVariant(t *testing.T, resolver *Resolver, group, module, version string) {
	t.Helper()
	umbrella := Coordinate{Group: group, Module: module, Version: version}
	umbrellaBase := resolver.moduleBasePath(umbrella) + "/hash"
	if err := os.MkdirAll(umbrellaBase, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{
		"variants": [
			{
				"name": "metadataApiElements",
				"attributes": {
					"org.jetbrains.kotlin.platform.type": "common",
					"org.gradle.usage": "kotlin-metadata"
				},
				"files": [{"name":"%s-%s.jar","url":"%s-%s.jar"}]
			},
			{
				"name": "jvmRuntimeElements-published",
				"attributes": {
					"org.jetbrains.kotlin.platform.type": "jvm",
					"org.gradle.usage": "java-runtime"
				},
				"available-at": {"group":"%s","module":"%s-jvm","version":"%s"}
			},
			{
				"name": "jvmApiElements-published",
				"attributes": {
					"org.jetbrains.kotlin.platform.type": "jvm",
					"org.gradle.usage": "java-api"
				},
				"available-at": {"group":"%s","module":"%s-jvm","version":"%s"}
			}
		]
	}`, module, version, module, version, group, module, version, group, module, version)
	writeModuleAt(t, umbrellaBase, module, version, body)
	stageJVMVariantArtifact(t, resolver, group, module+"-jvm", version)
}

func stageJVMVariantArtifact(t *testing.T, resolver *Resolver, group, module, version string) {
	t.Helper()
	target := Coordinate{Group: group, Module: module, Version: version}
	base := resolver.moduleBasePath(target) + "/hash"
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{
		"variants": [{
			"name": "jvmRuntimeElements-published",
			"attributes": {
				"org.jetbrains.kotlin.platform.type": "jvm",
				"org.gradle.usage": "java-runtime"
			},
			"files": [{"name":"%s-%s.jar","url":"%s-%s.jar"}]
		}]
	}`, module, version, module, version)
	writeModuleAt(t, base, module, version, body)
	if err := os.WriteFile(filepath.Join(base, fmt.Sprintf("%s-%s.jar", module, version)), []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// minimalAAR returns the bytes of a structurally-valid Android library
// archive: a zip containing AndroidManifest.xml and an empty
// classes.jar. The resolver's extractAAR only needs the zip to open
// cleanly and to produce the entries it expects to find.
func minimalAAR(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	manifest, err := w.Create("AndroidManifest.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Write([]byte(`<manifest package="com.example.fake"/>`)); err != nil {
		t.Fatal(err)
	}
	classes, err := w.Create("classes.jar")
	if err != nil {
		t.Fatal(err)
	}
	// Empty inner zip is a valid (though useless) jar.
	var inner bytes.Buffer
	iw := zip.NewWriter(&inner)
	if err := iw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := classes.Write(inner.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeModuleAt(t *testing.T, base, module, version, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(base, fmt.Sprintf("%s-%s.module", module, version)), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
