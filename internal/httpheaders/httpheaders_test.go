package httpheaders

import (
	"net/http"
	"os"
	"testing"
)

func TestSetApplyStaticHeaders(t *testing.T) {
	var s Set
	s.AddStatic("X-Tag", "internal")
	s.AddStatic("Authorization", "Bearer abc")
	h := http.Header{}
	s.Apply(h)
	if got := h.Get("X-Tag"); got != "internal" {
		t.Fatalf("X-Tag: got %q want internal", got)
	}
	if got := h.Get("Authorization"); got != "Bearer abc" {
		t.Fatalf("Authorization: got %q want Bearer abc", got)
	}
}

func TestSetApplyStaticMap(t *testing.T) {
	var s Set
	s.AddStaticMap(map[string]string{"X-A": "1", "X-B": "2"})
	h := http.Header{}
	s.Apply(h)
	if h.Get("X-A") != "1" || h.Get("X-B") != "2" {
		t.Fatalf("static map not applied: %#v", h)
	}
}

func TestSetSkipsEmptyValues(t *testing.T) {
	var s Set
	s.AddStatic("X-Empty", "")
	s.AddStatic("X-Present", "v")
	h := http.Header{}
	s.Apply(h)
	if _, ok := h["X-Empty"]; ok {
		t.Fatalf("expected empty static value to be skipped, got %#v", h)
	}
	if h.Get("X-Present") != "v" {
		t.Fatalf("X-Present: got %q want v", h.Get("X-Present"))
	}
}

func TestSetIgnoresEmptyHeaderName(t *testing.T) {
	var s Set
	s.AddStatic("", "v")
	s.AddEnv("", "ANY")
	s.AddEnv("X-Header", "")
	h := http.Header{}
	s.Apply(h)
	if len(h) != 0 {
		t.Fatalf("expected empty header set, got %#v", h)
	}
}

func TestSetEnvResolvedAtApply(t *testing.T) {
	var s Set
	s.AddEnv("Authorization", "TEST_HTTPHEADERS_TOKEN")

	t.Setenv("TEST_HTTPHEADERS_TOKEN", "Bearer first")
	h1 := http.Header{}
	s.Apply(h1)
	if got := h1.Get("Authorization"); got != "Bearer first" {
		t.Fatalf("first apply: got %q", got)
	}

	t.Setenv("TEST_HTTPHEADERS_TOKEN", "Bearer second")
	h2 := http.Header{}
	s.Apply(h2)
	if got := h2.Get("Authorization"); got != "Bearer second" {
		t.Fatalf("second apply: got %q (env rotation should reflect)", got)
	}
}

func TestSetEnvSkippedWhenUnsetOrEmpty(t *testing.T) {
	var s Set
	s.AddEnv("Authorization", "TEST_HTTPHEADERS_UNSET")
	if _, ok := os.LookupEnv("TEST_HTTPHEADERS_UNSET"); ok {
		t.Skip("unexpected pre-existing env var")
	}
	h := http.Header{}
	s.Apply(h)
	if _, ok := h["Authorization"]; ok {
		t.Fatalf("expected Authorization unset, got %#v", h)
	}
}

func TestSetEnvOverridesStaticOfSameName(t *testing.T) {
	var s Set
	s.AddStatic("Authorization", "static-placeholder")
	s.AddEnv("Authorization", "TEST_HTTPHEADERS_OVERRIDE")
	t.Setenv("TEST_HTTPHEADERS_OVERRIDE", "Bearer real")
	h := http.Header{}
	s.Apply(h)
	if got := h.Get("Authorization"); got != "Bearer real" {
		t.Fatalf("env should override static placeholder, got %q", got)
	}
}
