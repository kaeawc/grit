package javadoc

import (
	"path/filepath"
	"testing"
)

func TestSelectToolReturnsJavadocForPureJava(t *testing.T) {
	roots := []string{
		filepath.Join("src", "main", "java"),
	}
	if got := SelectTool(roots); got != ToolKindJavadoc {
		t.Fatalf("expected javadoc, got %s", got)
	}
}

func TestSelectToolReturnsDokkaForKotlinDir(t *testing.T) {
	roots := []string{
		filepath.Join("src", "main", "kotlin"),
	}
	if got := SelectTool(roots); got != ToolKindDokka {
		t.Fatalf("expected dokka, got %s", got)
	}
}

func TestSelectToolReturnsDokkaForKtFile(t *testing.T) {
	roots := []string{
		filepath.Join("src", "main", "java"),
		filepath.Join("src", "main", "kotlin", "Foo.kt"),
	}
	if got := SelectTool(roots); got != ToolKindDokka {
		t.Fatalf("expected dokka, got %s", got)
	}
}

func TestSelectToolReturnsDokkaForMixed(t *testing.T) {
	roots := []string{
		filepath.Join("src", "main", "java"),
		filepath.Join("src", "main", "kotlin"),
	}
	if got := SelectTool(roots); got != ToolKindDokka {
		t.Fatalf("expected dokka, got %s", got)
	}
}

func TestSelectToolReturnsJavadocForEmpty(t *testing.T) {
	if got := SelectTool(nil); got != ToolKindJavadoc {
		t.Fatalf("expected javadoc for empty roots, got %s", got)
	}
}

func TestClassifier(t *testing.T) {
	if got := Classifier(); got != "javadoc" {
		t.Fatalf("expected javadoc, got %s", got)
	}
}

func TestOutputFileName(t *testing.T) {
	got := OutputFileName("mylib", "1.0.0")
	want := "mylib-1.0.0-javadoc.jar"
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestOutputPathForModule(t *testing.T) {
	got := OutputPathForModule(filepath.Join("project", "mylib"), "mylib", "1.0.0")
	want := filepath.Join("project", "mylib", "build", "libs", "mylib-1.0.0-javadoc.jar")
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestNewToolDescriptorAutoSelectsDokka(t *testing.T) {
	roots := []string{filepath.Join("src", "main", "kotlin")}
	cp := []string{"lib.jar"}
	out := filepath.Join("build", "libs", "foo-1.0-javadoc.jar")

	td := NewToolDescriptor(roots, cp, out)
	if td.Tool != ToolKindDokka {
		t.Fatalf("expected dokka, got %s", td.Tool)
	}
	if td.OutputPath != out {
		t.Fatalf("output path mismatch: %s", td.OutputPath)
	}
	if len(td.Classpath) != 1 || td.Classpath[0] != "lib.jar" {
		t.Fatalf("classpath mismatch: %v", td.Classpath)
	}
}

func TestNewToolDescriptorAutoSelectsJavadoc(t *testing.T) {
	roots := []string{filepath.Join("src", "main", "java")}
	td := NewToolDescriptor(roots, nil, "out.jar")
	if td.Tool != ToolKindJavadoc {
		t.Fatalf("expected javadoc, got %s", td.Tool)
	}
}
