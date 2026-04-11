package nativecompile

import (
	"strings"
	"testing"
)

func TestKotlinToolchainValidateRequiresExplicitCompilerArtifacts(t *testing.T) {
	toolchain := &kotlinToolchain{
		Version: "2.3.3",
		CompilerClasspath: []string{
			"/tmp/kotlin-compiler-embeddable-2.3.3.jar",
			"/tmp/kotlin-stdlib-2.3.3.jar",
		},
	}
	err := toolchain.validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "kotlin-script-runtime:2.3.3") || !strings.Contains(msg, "org.jetbrains:annotations") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestKotlinToolchainValidateAcceptsExplicitCompilerClasspath(t *testing.T) {
	toolchain := &kotlinToolchain{
		Version: "2.3.3",
		CompilerClasspath: []string{
			"/tmp/kotlin-compiler-embeddable-2.3.3.jar",
			"/tmp/kotlin-stdlib-2.3.3.jar",
			"/tmp/kotlin-script-runtime-2.3.3.jar",
			"/tmp/annotations-13.0.jar",
		},
	}
	if err := toolchain.validate(); err != nil {
		t.Fatalf("expected valid toolchain, got %v", err)
	}
}
