package nativecompile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"unicode"
)

func semanticSourceFingerprintsMatch(path string, sources []string) bool {
	want, err := readSemanticSourceFingerprints(path)
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := semanticSourceFingerprints(sources)
	if err != nil || len(got) != len(want) {
		return false
	}
	for source, digest := range got {
		if want[source] != digest {
			return false
		}
	}
	return true
}

func writeSemanticSourceFingerprints(path string, sources []string) error {
	fingerprints, err := semanticSourceFingerprints(sources)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(fingerprints, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func readSemanticSourceFingerprints(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func semanticSourceFingerprints(sources []string) (map[string]string, error) {
	out := make(map[string]string, len(sources))
	for _, source := range sources {
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		out[source] = semanticSourceFingerprint(data)
	}
	return out, nil
}

func semanticSourceFingerprint(data []byte) string {
	normalized := stripKotlinJavaCommentsAndWhitespace(string(data))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func stripKotlinJavaCommentsAndWhitespace(input string) string {
	var out strings.Builder
	inLineComment := false
	inBlockComment := false
	inString := false
	inTripleString := false
	inChar := false
	escaped := false
	runes := []rune(input)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		next := rune(0)
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		next2 := rune(0)
		if i+2 < len(runes) {
			next2 = runes[i+2]
		}
		if inLineComment {
			if r == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			if r == '*' && next == '/' {
				inBlockComment = false
				i++
			}
			continue
		}
		if inTripleString {
			out.WriteRune(r)
			if r == '"' && next == '"' && next2 == '"' {
				out.WriteRune(next)
				out.WriteRune(next2)
				i += 2
				inTripleString = false
			}
			continue
		}
		if inString {
			out.WriteRune(r)
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		if inChar {
			out.WriteRune(r)
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '\'' {
				inChar = false
			}
			continue
		}
		switch {
		case r == '/' && next == '/':
			inLineComment = true
			i++
		case r == '/' && next == '*':
			inBlockComment = true
			i++
		case r == '"' && next == '"' && next2 == '"':
			inTripleString = true
			out.WriteRune(r)
			out.WriteRune(next)
			out.WriteRune(next2)
			i += 2
		case r == '"':
			inString = true
			out.WriteRune(r)
		case r == '\'':
			inChar = true
			out.WriteRune(r)
		case unicode.IsSpace(r):
			continue
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}
