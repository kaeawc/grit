package griterr

import (
	"errors"
	"fmt"
	"testing"
)

func TestNew(t *testing.T) {
	err := New(ErrUnsupported, "tree-sitter language \"groovy\"")
	if err.Kind != ErrUnsupported {
		t.Fatalf("kind = %q, want %q", err.Kind, ErrUnsupported)
	}
	if err.Message != "tree-sitter language \"groovy\"" {
		t.Fatalf("message = %q", err.Message)
	}
	if err.Cause != nil {
		t.Fatal("cause should be nil")
	}
	want := "unsupported: tree-sitter language \"groovy\""
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestNewf(t *testing.T) {
	err := Newf(ErrInvalidInput, "module %q not found", ":app")
	if err.Kind != ErrInvalidInput {
		t.Fatalf("kind = %q, want %q", err.Kind, ErrInvalidInput)
	}
	want := "invalid_input: module \":app\" not found"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestWrap(t *testing.T) {
	cause := errors.New("connection refused")
	err := Wrap(ErrToolFailure, "aapt2 link", cause)
	if err.Kind != ErrToolFailure {
		t.Fatalf("kind = %q, want %q", err.Kind, ErrToolFailure)
	}
	want := "tool_failure: aapt2 link: connection refused"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is should find the wrapped cause")
	}
}

func TestKindOf(t *testing.T) {
	t.Run("direct", func(t *testing.T) {
		err := New(ErrCacheState, "corrupt entry")
		kind, ok := KindOf(err)
		if !ok || kind != ErrCacheState {
			t.Fatalf("KindOf = (%q, %v), want (%q, true)", kind, ok, ErrCacheState)
		}
	})

	t.Run("wrapped", func(t *testing.T) {
		inner := New(ErrMissingState, "no signing config")
		wrapped := fmt.Errorf("build failed: %w", inner)
		kind, ok := KindOf(wrapped)
		if !ok || kind != ErrMissingState {
			t.Fatalf("KindOf = (%q, %v), want (%q, true)", kind, ok, ErrMissingState)
		}
	})

	t.Run("plain error", func(t *testing.T) {
		err := errors.New("something broke")
		kind, ok := KindOf(err)
		if ok {
			t.Fatalf("KindOf = (%q, true), want (\"\", false)", kind)
		}
	})

	t.Run("nil", func(t *testing.T) {
		kind, ok := KindOf(nil)
		if ok {
			t.Fatalf("KindOf(nil) = (%q, true), want (\"\", false)", kind)
		}
	})
}

func TestErrorSatisfiesInterface(t *testing.T) {
	var err error = New(ErrUnsupported, "test") //nolint:staticcheck
	if err == nil {
		t.Fatal("should satisfy error interface")
	}
}
