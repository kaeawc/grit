// Package griterr provides structured error types for grit.
//
// Errors carry a Kind that classifies the failure category (invalid input,
// unsupported feature, tool failure, etc.) so that presentation layers can
// report failures consistently without coupling to low-level package details.
package griterr

import (
	"errors"
	"fmt"
)

// Kind classifies a grit error into a stable category that presentation
// layers (CLI JSON, IDE bridge) can use for consistent reporting.
type Kind string

const (
	ErrInvalidInput Kind = "invalid_input"
	ErrUnsupported  Kind = "unsupported"
	ErrToolFailure  Kind = "tool_failure"
	ErrCacheState   Kind = "cache_state"
	ErrMissingState Kind = "missing_state"
)

// Error is a structured error that carries a classification Kind alongside
// the human-readable message and an optional wrapped cause.
type Error struct {
	Kind    Kind
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

// New creates a structured error with the given kind and message.
func New(kind Kind, msg string) *Error {
	return &Error{Kind: kind, Message: msg}
}

// Wrap creates a structured error that wraps an existing error.
func Wrap(kind Kind, msg string, cause error) *Error {
	return &Error{Kind: kind, Message: msg, Cause: cause}
}

// Newf creates a structured error with a formatted message.
func Newf(kind Kind, format string, args ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// KindOf extracts the Kind from an error if it is or wraps a *Error.
// Returns the kind and true if found, or ("", false) otherwise.
func KindOf(err error) (Kind, bool) {
	var ge *Error
	if errors.As(err, &ge) {
		return ge.Kind, true
	}
	return "", false
}
