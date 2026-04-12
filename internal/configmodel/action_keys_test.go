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
