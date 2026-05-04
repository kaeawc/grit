package configmodel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/lint"
	"github.com/kaeawc/grit/internal/project"
)

func TestLintActionCacheKeyRegistered(t *testing.T) {
	fn, ok := actionCacheKeyRegistry[graph.ActionKindLint]
	if !ok || fn == nil {
		t.Fatalf("expected lint action cache key to be registered, got %#v", actionCacheKeyRegistry)
	}
}

func TestRegisterActionCacheKeyAddsNewKind(t *testing.T) {
	kind := graph.ActionKind("lint-test-register")
	delete(actionCacheKeyRegistry, kind)
	t.Cleanup(func() {
		delete(actionCacheKeyRegistry, kind)
	})

	registerActionCacheKey(kind, func(_ *Model, _ graph.Action) string {
		return "registered-cache-key"
	})

	if got := actionCacheKeyForModel(nil, graph.Action{Kind: kind}); got != "registered-cache-key" {
		t.Fatalf("registered cache key = %q, want %q", got, "registered-cache-key")
	}
}

func TestRegisterActionCacheKeyRejectsUnknownKind(t *testing.T) {
	assertPanics(t, func() {
		registerActionCacheKey(graph.ActionKindUnknown, func(_ *Model, _ graph.Action) string {
			return "ignored"
		})
	})
}

func TestRegisterActionCacheKeyRejectsNilFunction(t *testing.T) {
	assertPanics(t, func() {
		registerActionCacheKey(graph.ActionKind("lint-test-nil"), nil)
	})
}

func TestRegisterActionCacheKeyRejectsDuplicateKind(t *testing.T) {
	assertPanics(t, func() {
		registerActionCacheKey(graph.ActionKindLint, func(_ *Model, _ graph.Action) string {
			return "duplicate"
		})
	})
}

func TestLintActionCacheKeyUsesModuleLintFiles(t *testing.T) {
	moduleDir := t.TempDir()
	sourceRoot := filepath.Join(moduleDir, "src", "main")
	mustWriteActionKeyFile(t, filepath.Join(sourceRoot, "java", "MainActivity.kt"), "class MainActivity")
	mustWriteActionKeyFile(t, filepath.Join(moduleDir, "lint.xml"), "<lint/>")
	mustWriteActionKeyFile(t, filepath.Join(moduleDir, "lint-baseline.xml"), "<issues/>")

	model := &Model{
		Summary: project.SemanticGraphSummary{
			Modules: []project.SemanticModuleSummary{{
				Path: ":app",
				Dir:  moduleDir,
				Variants: []project.SemanticVariantSummary{{
					Name: "debug",
					Materialization: project.SemanticMaterializationSummary{
						SourceRoots: []string{sourceRoot},
					},
				}},
			}},
		},
	}
	action := graph.Action{
		Kind: graph.ActionKindLint,
		Attributes: map[string]string{
			"modulePath":  ":app",
			"variantName": "debug",
		},
	}

	resolved, ok := model.ResolvedVariant(":app", "debug")
	if !ok {
		t.Fatal("expected resolved variant")
	}
	want := lint.ActionFromVariantInModule(resolved, moduleDir).CacheKey().String()
	if got := lintActionCacheKey(model, action); got != want {
		t.Fatalf("lint cache key = %q, want %q", got, want)
	}

	initial := lintActionCacheKey(model, action)
	if err := os.Remove(filepath.Join(moduleDir, "lint.xml")); err != nil {
		t.Fatal(err)
	}
	if got := lintActionCacheKey(model, action); got == initial {
		t.Fatal("adding or removing discovered lint.xml must change cache key")
	}
}

func TestLintActionCacheKeyUsesDependencyInputs(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	libDir := filepath.Join(root, "lib")
	appSourceRoot := filepath.Join(appDir, "src", "main")
	libSourceRoot := filepath.Join(libDir, "src", "main")
	mustWriteActionKeyFile(t, filepath.Join(appSourceRoot, "java", "MainActivity.kt"), "class MainActivity")
	libSource := filepath.Join(libSourceRoot, "java", "Lib.kt")
	mustWriteActionKeyFile(t, libSource, "class Lib")

	model := &Model{
		Summary: project.SemanticGraphSummary{
			Modules: []project.SemanticModuleSummary{
				{
					Path: ":app",
					Dir:  appDir,
					Variants: []project.SemanticVariantSummary{{
						Name: "debug",
						Materialization: project.SemanticMaterializationSummary{
							ID:          "materialization:app:debug",
							SourceRoots: []string{appSourceRoot},
						},
					}},
				},
				{
					Path: ":lib",
					Dir:  libDir,
					Variants: []project.SemanticVariantSummary{{
						Name: "debug",
						Materialization: project.SemanticMaterializationSummary{
							ID:          "materialization:lib:debug",
							SourceRoots: []string{libSourceRoot},
						},
					}},
				},
			},
		},
		ArtifactSummaries: []ArtifactSummary{
			{
				ID:                "artifact:app:sources",
				ModulePath:        ":app",
				VariantName:       "debug",
				MaterializationID: "materialization:app:debug",
				Kind:              string(graph.ArtifactKindDirectory),
				Path:              appSourceRoot,
			},
			{
				ID:                "artifact:lib:sources",
				ModulePath:        ":lib",
				VariantName:       "debug",
				MaterializationID: "materialization:lib:debug",
				Kind:              string(graph.ArtifactKindDirectory),
				Path:              libSourceRoot,
			},
		},
		ProvenanceSummaries: []ProvenanceSummary{
			{
				MaterializationID: "materialization:app:debug",
				ModulePath:        ":app",
				VariantName:       "debug",
				SourceRoots:       []string{appSourceRoot},
				ManifestPaths:     []string{filepath.Join(appDir, "src", "main", "AndroidManifest.xml")},
			},
			{
				MaterializationID: "materialization:lib:debug",
				ModulePath:        ":lib",
				VariantName:       "debug",
				SourceRoots:       []string{libSourceRoot},
				ManifestPaths:     []string{filepath.Join(libDir, "src", "main", "AndroidManifest.xml")},
			},
		},
	}
	action := graph.Action{
		Kind: graph.ActionKindLint,
		Inputs: []graph.ArtifactID{
			graph.ArtifactID("artifact:app:sources"),
			graph.ArtifactID("artifact:lib:sources"),
		},
		Attributes: map[string]string{
			"modulePath":  ":app",
			"variantName": "debug",
		},
	}

	before := lintActionCacheKey(model, action)
	mustWriteActionKeyFile(t, libSource, "class LibChanged")
	after := lintActionCacheKey(model, action)
	if before == after {
		t.Fatal("changing dependency input content must change lint cache key")
	}
}

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}

func mustWriteActionKeyFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
