package configmodel

import (
	"testing"

	"github.com/kaeawc/grit/internal/graph"
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

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}
