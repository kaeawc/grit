package treesitter

import (
	"github.com/kaeawc/grit/internal/griterr"
	tskotlin "github.com/kaeawc/grit/internal/treesitter/kotlin"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tsjava "github.com/tree-sitter/tree-sitter-java/bindings/go"
)

type Language string

const (
	Kotlin Language = "kotlin"
	Java   Language = "java"
	Groovy Language = "groovy"
)

func Parse(lang Language, src []byte) (*sitter.Tree, error) {
	parser := sitter.NewParser()
	language, err := languageFor(lang)
	if err != nil {
		parser.Close()
		return nil, err
	}
	if err := parser.SetLanguage(language); err != nil {
		parser.Close()
		return nil, err
	}
	tree := parser.Parse(src, nil)
	parser.Close()
	return tree, nil
}

func languageFor(lang Language) (*sitter.Language, error) {
	switch lang {
	case Kotlin:
		return sitter.NewLanguage(tskotlin.Language()), nil
	case Java:
		return sitter.NewLanguage(tsjava.Language()), nil
	default:
		return nil, griterr.Newf(griterr.ErrUnsupported, "tree-sitter language %q", lang)
	}
}
