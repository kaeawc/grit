package treesitter

import "testing"

func TestParseKotlin(t *testing.T) {
	tree, err := Parse(Kotlin, []byte(`rootProject.name = "Demo"`))
	if err != nil {
		t.Fatal(err)
	}
	if tree == nil || tree.RootNode().Kind() != "source_file" {
		t.Fatalf("unexpected tree: %#v", tree)
	}
	tree.Close()
}

func TestParseJava(t *testing.T) {
	tree, err := Parse(Java, []byte(`class Demo {}`))
	if err != nil {
		t.Fatal(err)
	}
	if tree == nil || tree.RootNode().Kind() != "program" {
		t.Fatalf("unexpected tree: %#v", tree)
	}
	tree.Close()
}

func TestParseUnsupportedLanguageReturnsError(t *testing.T) {
	tree, err := Parse(Groovy, []byte(`println "hi"`))
	if err == nil {
		t.Fatal("expected unsupported language error")
	}
	if tree != nil {
		t.Fatalf("expected nil tree, got %#v", tree)
	}
}
