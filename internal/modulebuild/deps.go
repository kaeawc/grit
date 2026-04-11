package modulebuild

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode"
)

type Dependencies struct {
	Main                   []Ref
	Debug                  []Ref
	Test                   []Ref
	AndroidTest            []Ref
	CompileOnly            []Ref
	RuntimeOnly            []Ref
	TestCompileOnly        []Ref
	TestRuntimeOnly        []Ref
	AndroidTestCompileOnly []Ref
	AndroidTestRuntimeOnly []Ref
	CoreLibraryDesugaring  []Ref
	Scoped                 map[string][]Ref
}

type Ref struct {
	Kind  string
	Value string
}

func ParseDependencies(buildFile string) (*Dependencies, error) {
	data, err := os.ReadFile(buildFile)
	if err != nil {
		return nil, err
	}
	body := string(data)

	block, err := extractDependenciesBlock(body)
	if err != nil {
		if strings.Contains(err.Error(), "dependencies block not found") {
			return &Dependencies{}, nil
		}
		return nil, err
	}

	deps := &Dependencies{}
	re := regexp.MustCompile(`(?m)^\s*([A-Za-z][A-Za-z0-9_]*)\((.+?)\)\s*(?:\{.*\})?\s*$`)
	cleanBlock := stripDependencyComments(block)
	for _, match := range re.FindAllStringSubmatch(cleanBlock, -1) {
		scope := match[1]
		ref := parseRef(match[2])
		if deps.Scoped == nil {
			deps.Scoped = map[string][]Ref{}
		}
		deps.Scoped[scope] = append(deps.Scoped[scope], ref)
		switch scope {
		case "api", "implementation":
			deps.Main = append(deps.Main, ref)
		case "debugImplementation":
			deps.Debug = append(deps.Debug, ref)
		case "testImplementation":
			deps.Test = append(deps.Test, ref)
		case "unitTestImplementation":
			deps.Test = append(deps.Test, ref)
		case "androidTestImplementation":
			deps.AndroidTest = append(deps.AndroidTest, ref)
		case "compileOnly":
			deps.CompileOnly = append(deps.CompileOnly, ref)
		case "runtimeOnly":
			deps.RuntimeOnly = append(deps.RuntimeOnly, ref)
		case "testCompileOnly":
			deps.TestCompileOnly = append(deps.TestCompileOnly, ref)
		case "unitTestCompileOnly":
			deps.TestCompileOnly = append(deps.TestCompileOnly, ref)
		case "androidTestCompileOnly":
			deps.AndroidTestCompileOnly = append(deps.AndroidTestCompileOnly, ref)
		case "testRuntimeOnly":
			deps.TestRuntimeOnly = append(deps.TestRuntimeOnly, ref)
		case "unitTestRuntimeOnly":
			deps.TestRuntimeOnly = append(deps.TestRuntimeOnly, ref)
		case "androidTestRuntimeOnly":
			deps.AndroidTestRuntimeOnly = append(deps.AndroidTestRuntimeOnly, ref)
		case "coreLibraryDesugaring":
			deps.CoreLibraryDesugaring = append(deps.CoreLibraryDesugaring, ref)
		}
	}
	return deps, nil
}

func stripDependencyComments(block string) string {
	var lines []string
	for _, line := range strings.Split(block, "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func extractDependenciesBlock(body string) (string, error) {
	start := strings.Index(body, "dependencies {")
	if start < 0 {
		return "", fmt.Errorf("dependencies block not found")
	}
	rest := body[start:]
	depth := 0
	for i, r := range rest {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[:i+1], nil
			}
		}
	}
	return "", fmt.Errorf("unterminated dependencies block")
}

func parseRef(expr string) Ref {
	expr = strings.TrimSpace(expr)
	expr = stripTrailingClosure(expr)
	if strings.HasPrefix(expr, "platform(") && strings.HasSuffix(expr, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(expr, "platform("), ")")
		ref := parseRef(inner)
		ref.Kind = "platform-" + ref.Kind
		return ref
	}
	if inner, ok := unwrapCall(expr, "files"); ok {
		return parseRef(inner)
	}
	if inner, ok := unwrapCall(expr, "fileTree"); ok {
		return parseRef(inner)
	}
	switch {
	case strings.HasPrefix(expr, "libs.bundles."):
		return Ref{Kind: "bundle", Value: strings.TrimPrefix(expr, "libs.bundles.")}
	case strings.HasPrefix(expr, "libs."):
		return Ref{Kind: "library", Value: strings.TrimPrefix(expr, "libs.")}
	case strings.HasPrefix(expr, "project(\"") && strings.HasSuffix(expr, "\")"):
		return Ref{Kind: "project", Value: strings.TrimSuffix(strings.TrimPrefix(expr, "project(\""), "\")")}
	case strings.HasPrefix(expr, "projects."):
		parts := strings.Split(strings.TrimPrefix(expr, "projects."), ".")
		for i, part := range parts {
			parts[i] = camelToKebab(part)
		}
		return Ref{Kind: "project", Value: ":" + strings.Join(parts, ":")}
	case isQuotedString(expr):
		return Ref{Kind: "raw", Value: strings.Trim(expr, `"`)}
	default:
		return Ref{Kind: "raw", Value: expr}
	}
}

func stripTrailingClosure(expr string) string {
	expr = strings.TrimSpace(expr)
	if !strings.HasSuffix(expr, "}") {
		return expr
	}
	depth := 0
	for i := len(expr) - 1; i >= 0; i-- {
		switch expr[i] {
		case '}':
			depth++
		case '{':
			depth--
			if depth == 0 {
				prefix := strings.TrimSpace(expr[:i])
				if strings.HasSuffix(prefix, ")") {
					return prefix
				}
				return expr
			}
		}
	}
	return expr
}

func unwrapCall(expr, name string) (string, bool) {
	prefix := name + "("
	if !strings.HasPrefix(expr, prefix) || !strings.HasSuffix(expr, ")") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(expr, prefix), ")")), true
}

func isQuotedString(expr string) bool {
	return len(expr) >= 2 && strings.HasPrefix(expr, `"`) && strings.HasSuffix(expr, `"`)
}

func camelToKebab(s string) string {
	var out []rune
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				out = append(out, '-')
			}
			out = append(out, unicode.ToLower(r))
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
