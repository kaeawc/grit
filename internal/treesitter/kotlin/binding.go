package kotlin

// #cgo CFLAGS: -std=c11 -fPIC -Isrc
// #include "src/parser.c"
// #include "src/scanner.c"
import "C"

import "unsafe"

func Language() unsafe.Pointer {
	return unsafe.Pointer(C.tree_sitter_kotlin())
}
