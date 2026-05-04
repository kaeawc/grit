package nativecompile

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

func TestProjectKSPVersionPrefersCatalogKey(t *testing.T) {
	prj := &project.Project{
		VersionCatalogData: map[string]string{
			"build-kotlin-ksp": "2.1.20-1.0.32",
		},
	}
	if got, want := projectKSPVersion(prj), "2.1.20-1.0.32"; got != want {
		t.Fatalf("projectKSPVersion: got %q want %q", got, want)
	}
}

func TestProjectKSPVersionFallsBackThroughKnownKeys(t *testing.T) {
	prj := &project.Project{
		VersionCatalogData: map[string]string{"ksp": "1.9.24-1.0.20"},
	}
	if got, want := projectKSPVersion(prj), "1.9.24-1.0.20"; got != want {
		t.Fatalf("ksp key: got %q want %q", got, want)
	}
}

func TestKSPPluginOptionsContainsRequiredKeys(t *testing.T) {
	procCP := []string{"/m2/glide-ksp.jar", "/m2/glide-annotations.jar"}
	opts := kspPluginOptions(
		procCP,
		"/proj/glide-config",
		"/out/ksp/kotlin",
		"/out/ksp/java",
		"/out/ksp/resources",
		"/out/classes",
		"/out/ksp/caches",
		map[string]string{"glide.generated.module.package": "org.signal"},
	)
	want := []string{
		"apclasspath=",
		"projectBaseDir=/proj/glide-config",
		"classOutputDir=/out/classes",
		"javaOutputDir=/out/ksp/java",
		"kotlinOutputDir=/out/ksp/kotlin",
		"resourceOutputDir=/out/ksp/resources",
		"kspOutputDir=/out/ksp",
		"cachesDir=/out/ksp/caches",
		"incremental=false",
		"withCompilation=true",
		"apoption=glide.generated.module.package=org.signal",
	}
	joined := strings.Join(opts, "\n")
	for _, fragment := range want {
		if !strings.Contains(joined, fragment) {
			t.Fatalf("missing option fragment %q in:\n%s", fragment, joined)
		}
	}
	idPrefix := "plugin:" + modulebuild.KSPCompilerPluginID + ":"
	for _, opt := range opts {
		if !strings.HasPrefix(opt, idPrefix) {
			t.Fatalf("option missing plugin id prefix: %q", opt)
		}
	}
	for _, opt := range opts {
		if strings.HasPrefix(opt, idPrefix+"apclasspath=") {
			cp := strings.TrimPrefix(opt, idPrefix+"apclasspath=")
			if !strings.Contains(cp, "/m2/glide-ksp.jar") || !strings.Contains(cp, "/m2/glide-annotations.jar") {
				t.Fatalf("apclasspath missing processor jars: %q", cp)
			}
		}
	}
}

func TestKSPOutputRootsLayout(t *testing.T) {
	prj := &project.Project{RootDir: "/proj"}
	mod := &project.Module{Path: ":glide-config", Dir: "/proj/glide-config"}
	classOut := "/proj/build/grit/glide-config/debug/classes"
	root, kotlin, java, resources, classes, caches := kspOutputRoots(prj, mod, "debug", classOut)
	wantRoot := filepath.Join("/proj", "build", "grit", "glide-config", "debug", "ksp")
	if root != wantRoot {
		t.Fatalf("root: got %q want %q", root, wantRoot)
	}
	if kotlin != filepath.Join(wantRoot, "kotlin") {
		t.Fatalf("kotlin dir: got %q", kotlin)
	}
	if java != filepath.Join(wantRoot, "java") {
		t.Fatalf("java dir: got %q", java)
	}
	if resources != filepath.Join(wantRoot, "resources") {
		t.Fatalf("resources dir: got %q", resources)
	}
	if caches != filepath.Join(wantRoot, "caches") {
		t.Fatalf("caches dir: got %q", caches)
	}
	if classes != classOut {
		t.Fatalf("class dir should pass through unchanged: got %q want %q", classes, classOut)
	}
}

func TestKSPHashTokensDeterministic(t *testing.T) {
	refs := []modulebuild.Ref{
		{Kind: "raw", Value: "com.google.dagger:hilt-compiler:2.51"},
		{Kind: "library", Value: "glide.ksp"},
	}
	opts := map[string]string{
		"room.schemaLocation": "/schemas",
		"dagger.fastInit":     "enabled",
	}
	a := kspHashTokens("2.1.20-1.0.32", refs, opts)
	b := kspHashTokens("2.1.20-1.0.32", refs, opts)
	if strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Fatalf("non-deterministic hash tokens:\n%v\nvs\n%v", a, b)
	}
	c := kspHashTokens("2.1.20-1.0.33", refs, opts)
	if strings.Join(a, "\n") == strings.Join(c, "\n") {
		t.Fatalf("hash tokens should differ when version changes")
	}
}

func TestKSPHashTokensSortedRegardlessOfInputOrder(t *testing.T) {
	a := kspHashTokens("v1",
		[]modulebuild.Ref{{Kind: "raw", Value: "a:b:1"}, {Kind: "raw", Value: "a:c:1"}},
		nil,
	)
	b := kspHashTokens("v1",
		[]modulebuild.Ref{{Kind: "raw", Value: "a:c:1"}, {Kind: "raw", Value: "a:b:1"}},
		nil,
	)
	if strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Fatalf("tokens should be order-independent:\n%v\nvs\n%v", a, b)
	}
}

func TestKSPHashTokensEmpty(t *testing.T) {
	if got := kspHashTokens("", nil, nil); got != nil {
		t.Fatalf("empty input should produce nil tokens, got %v", got)
	}
}
