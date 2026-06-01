package m2local

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/catalog"
	"github.com/kaeawc/grit/internal/modulebuild"
)

func TestApplyExcludesFiltersMatchingDependency(t *testing.T) {
	t.Parallel()

	deps := []Coordinate{
		{Group: "com.android.support", Module: "support-compat", Version: "26.1.0"},
		{Group: "androidx.core", Module: "core", Version: "1.17.0"},
	}
	excludes := []Exclude{{Group: "com.android.support", Module: "*"}}

	got := applyExcludes(deps, excludes)
	if len(got) != 1 {
		t.Fatalf("unexpected filtered deps: %#v", got)
	}
	if got[0].Group != "androidx.core" || got[0].Module != "core" {
		t.Fatalf("unexpected remaining dep: %#v", got[0])
	}
}

func TestCoordinateIDIncludesExcludes(t *testing.T) {
	t.Parallel()

	id := coordinateID(Coordinate{
		Group:   "com.google.android.gms",
		Module:  "play-services-tasks",
		Version: "16.0.1",
		Excludes: []Exclude{
			{Group: "com.android.support", Module: "*"},
		},
	})
	if id != "maven:com.google.android.gms:play-services-tasks:16.0.1|exclude=com.android.support:*" {
		t.Fatalf("unexpected coordinate id: %s", id)
	}
}

func TestPreferJVMSiblingForMetadataOnlyJar(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	base := filepath.Join(root, "org.jetbrains.kotlinx", "kotlinx-datetime", "0.7.1")
	if err := os.MkdirAll(filepath.Join(base, "downloaded"), 0o755); err != nil {
		t.Fatal(err)
	}
	jarPath := filepath.Join(base, "downloaded", "kotlinx-datetime-0.7.1.jar")
	f, err := os.Create(jarPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	jvmBase := filepath.Join(root, "org.jetbrains.kotlinx", "kotlinx-datetime-jvm", "0.7.1")
	seedDownloadedJar(t, jvmBase, "kotlinx-datetime-jvm-0.7.1.jar")
	resolver := New(root, t.TempDir(), nil, nil)
	alt, ok := resolver.preferJVMSibling(Coordinate{Group: "org.jetbrains.kotlinx", Module: "kotlinx-datetime", Version: "0.7.1"})
	if !ok {
		t.Fatal("expected jvm sibling to be preferred")
	}
	if alt.Module != "kotlinx-datetime-jvm" {
		t.Fatalf("unexpected sibling module: %#v", alt)
	}
}

// TestPreferJVMSiblingIgnoresEmptySiblingDir is the coil-compose
// regression: an Android AAR (no top-level class files) must NOT be
// rerouted to a `<module>-jvm` sibling just because an empty directory
// exists there. grit's own failed fetch attempts leave empty
// `downloaded/` dirs behind; bare existence is not proof a platform
// sibling actually resolves.
func TestPreferJVMSiblingIgnoresEmptySiblingDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// Umbrella AAR present (an Android library): zip with classes.jar
	// inside but no top-level .class entries, mirroring a real .aar.
	base := filepath.Join(root, "io.coil-kt", "coil-compose", "2.7.0")
	seedDownloadedAAR(t, base, "coil-compose-2.7.0.aar")
	// Empty -jvm sibling dir, exactly the pollution a prior failed fetch
	// leaves behind.
	jvmBase := filepath.Join(root, "io.coil-kt", "coil-compose-jvm", "2.7.0")
	if err := os.MkdirAll(filepath.Join(jvmBase, "downloaded"), 0o755); err != nil {
		t.Fatal(err)
	}

	resolver := New(root, t.TempDir(), nil, nil)
	_, ok := resolver.preferJVMSibling(Coordinate{Group: "io.coil-kt", Module: "coil-compose", Version: "2.7.0"})
	if ok {
		t.Fatal("must NOT redirect to an empty -jvm sibling dir")
	}
}

func seedDownloadedJar(t *testing.T, base, name string) {
	t.Helper()
	dir := filepath.Join(base, "downloaded")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("com/example/Real.class")
	if err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("classfile"))
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func seedDownloadedAAR(t *testing.T, base, name string) {
	t.Helper()
	dir := filepath.Join(base, "downloaded")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if _, err := zw.Create("classes.jar"); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExpandRefsNormalizesMetadataOnlyRootToJVMSibling(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	base := filepath.Join(root, "org.jetbrains.kotlinx", "kotlinx-datetime", "0.7.1-0.6.x-compat")
	if err := os.MkdirAll(filepath.Join(base, "downloaded"), 0o755); err != nil {
		t.Fatal(err)
	}
	rootJar := filepath.Join(base, "downloaded", "kotlinx-datetime-0.7.1-0.6.x-compat.jar")
	f, err := os.Create(rootJar)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	jvmBase := filepath.Join(root, "org.jetbrains.kotlinx", "kotlinx-datetime-jvm", "0.7.1-0.6.x-compat")
	seedDownloadedJar(t, jvmBase, "kotlinx-datetime-jvm-0.7.1-0.6.x-compat.jar")
	resolver := New(root, t.TempDir(), nil, &catalog.Catalog{
		Libraries: map[string]catalog.Library{
			"kotlinx-datetime": {
				Group:   "org.jetbrains.kotlinx",
				Name:    "kotlinx-datetime",
				Version: "0.7.1-0.6.x-compat",
			},
		},
		Versions: map[string]string{},
		Bundles:  map[string][]string{},
	})
	coords, err := resolver.expandRefs([]modulebuild.Ref{{Kind: "library", Value: "kotlinx.datetime"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(coords) != 1 {
		t.Fatalf("unexpected coords: %#v", coords)
	}
	if coords[0].Module != "kotlinx-datetime-jvm" {
		t.Fatalf("expected jvm sibling, got %#v", coords[0])
	}
}

func TestResolveOneSkipsAndroidFallbackForAlreadySuffixedModule(t *testing.T) {
	t.Parallel()

	// A coordinate whose module already ends in -jvm or -android must
	// never reroute to <module>-android — otherwise an offline KMP
	// graph that already lives at the -jvm sibling will accidentally
	// adopt an unrelated <module>-jvm-android directory left over from
	// a prior probe.
	cacheRoot := t.TempDir()
	workRoot := t.TempDir()
	for _, base := range []string{
		"io.modelcontextprotocol/kotlin-sdk-jvm/0.8.1",
		"io.modelcontextprotocol/kotlin-sdk-jvm-android/0.8.1",
	} {
		if err := os.MkdirAll(filepath.Join(cacheRoot, base), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	resolver := New(cacheRoot, workRoot, nil, nil)
	resolver.Offline = true

	coord := Coordinate{Group: "io.modelcontextprotocol", Module: "kotlin-sdk-jvm", Version: "0.8.1"}
	_, _, _, _ = resolver.resolveOneDepth(coord, 0)

	for _, sel := range resolver.snapshotReport().Selections {
		if sel.Kind == "module_redirect" && strings.Contains(sel.Chosen, "kotlin-sdk-jvm-android") {
			t.Fatalf("unexpected -android redirect from already-suffixed coord: %+v", sel)
		}
	}
}

func TestPreferJVMSiblingKeepsRequestedVersionWhenPresent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	base := filepath.Join(root, "ai.koog", "prompt-executor-anthropic-client", "0.6.4")
	if err := os.MkdirAll(filepath.Join(base, "downloaded"), 0o755); err != nil {
		t.Fatal(err)
	}
	rootJar := filepath.Join(base, "downloaded", "prompt-executor-anthropic-client-0.6.4.jar")
	f, err := os.Create(rootJar)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	jvmRequested := filepath.Join(root, "ai.koog", "prompt-executor-anthropic-client-jvm", "0.6.4")
	seedDownloadedJar(t, jvmRequested, "prompt-executor-anthropic-client-jvm-0.6.4.jar")
	jvmLatest := filepath.Join(root, "ai.koog", "prompt-executor-anthropic-client-jvm", "0.7.3")
	seedDownloadedJar(t, jvmLatest, "prompt-executor-anthropic-client-jvm-0.7.3.jar")

	resolver := New(root, t.TempDir(), nil, nil)
	alt, ok := resolver.preferJVMSibling(Coordinate{Group: "ai.koog", Module: "prompt-executor-anthropic-client", Version: "0.6.4"})
	if !ok {
		t.Fatal("expected jvm sibling to be preferred")
	}
	if alt.Module != "prompt-executor-anthropic-client-jvm" || alt.Version != "0.6.4" {
		t.Fatalf("expected requested jvm sibling version, got %#v", alt)
	}
}
