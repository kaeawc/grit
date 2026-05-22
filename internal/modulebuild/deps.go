package modulebuild

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// depBinding tracks a name→expression substitution in scope.
type depBinding struct {
	name string
	expr string
}

var valDeclRe = regexp.MustCompile(`^val\s+([A-Za-z][A-Za-z0-9_]*)\s*=\s*(.+)$`)
var bareIdentRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// expandDependencyBindings rewrites a full dependencies { ... } block so that
// let/also/apply/run/with lambda blocks and top-level val declarations are
// substituted inline, producing only flat scope(expr) lines for the regex.
func expandDependencyBindings(block string) string {
	inner := extractBlockInner(block)
	return expandStmts(inner, nil)
}

func extractBlockInner(block string) string {
	start := strings.Index(block, "{")
	if start < 0 {
		return block
	}
	end := strings.LastIndex(block, "}")
	if end <= start {
		return block
	}
	return block[start+1 : end]
}

// splitStatements splits the inner body of a block into top-level statements,
// keeping multi-line constructs (nested braces) together.
func splitStatements(body string) []string {
	var stmts []string
	var current strings.Builder
	depth := 0
	for _, r := range body {
		switch r {
		case '{':
			depth++
			current.WriteRune(r)
		case '}':
			depth--
			current.WriteRune(r)
		case '\n':
			if depth == 0 {
				if s := strings.TrimSpace(current.String()); s != "" {
					stmts = append(stmts, s)
				}
				current.Reset()
			} else {
				current.WriteRune(r)
			}
		default:
			current.WriteRune(r)
		}
	}
	if s := strings.TrimSpace(current.String()); s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}

func expandStmts(body string, bindings []depBinding) string {
	stmts := splitStatements(body)
	var lines []string
	local := make([]depBinding, len(bindings))
	copy(local, bindings)

	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		if name, expr, ok := parseValDecl(stmt); ok {
			local = append(local, depBinding{name, substituteBindings(expr, local)})
			continue
		}

		if receiver, param, inner, ok := parseLetAlso(stmt); ok {
			resolved := substituteBindings(receiver, local)
			child := append(append([]depBinding(nil), local...), depBinding{param, resolved})
			lines = append(lines, expandStmts(inner, child))
			continue
		}

		if receiver, inner, ok := parseApplyRun(stmt); ok {
			resolved := substituteBindings(receiver, local)
			child := append(append([]depBinding(nil), local...), depBinding{"this", resolved})
			lines = append(lines, expandStmts(inner, child))
			continue
		}

		if expr, inner, ok := parseWith(stmt); ok {
			resolved := substituteBindings(expr, local)
			child := append(append([]depBinding(nil), local...), depBinding{"this", resolved})
			lines = append(lines, expandStmts(inner, child))
			continue
		}

		lines = append(lines, substituteBindings(stmt, local))
	}
	return strings.Join(lines, "\n")
}

// findAtParenDepth0 finds the first occurrence of substr in s while at
// parenthesis depth 0 (i.e., not inside any '(' ')' pair).
func findAtParenDepth0(s, substr string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && strings.HasPrefix(s[i:], substr) {
			return i
		}
	}
	return -1
}

// stripTrailingBlock strips the final "}" from s (after trimming trailing whitespace).
func stripTrailingBlock(s string) string {
	s = strings.TrimRight(s, " \t\n")
	if strings.HasSuffix(s, "}") {
		return s[:len(s)-1]
	}
	return s
}

func parseValDecl(stmt string) (name, expr string, ok bool) {
	if strings.ContainsRune(stmt, '\n') {
		return
	}
	m := valDeclRe.FindStringSubmatch(stmt)
	if m == nil {
		return
	}
	return m[1], strings.TrimSpace(m[2]), true
}

// parseLetAlso matches: <receiver>.let { <param> -> <body> }
//
//	or: <receiver>.also { <param> -> <body> }
//
// where the receiver may itself contain nested parens.
func parseLetAlso(stmt string) (receiver, param, inner string, ok bool) {
	for _, kw := range []string{".let {", ".also {"} {
		idx := findAtParenDepth0(stmt, kw)
		if idx < 0 {
			continue
		}
		receiver = strings.TrimSpace(stmt[:idx])
		afterBrace := stmt[idx+len(kw):]
		arrowIdx := strings.Index(afterBrace, "->")
		var body string
		if arrowIdx >= 0 {
			param = strings.TrimSpace(afterBrace[:arrowIdx])
			body = afterBrace[arrowIdx+2:]
		} else {
			param = "it"
			body = afterBrace
		}
		inner = stripTrailingBlock(body)
		ok = true
		return
	}
	return
}

// parseApplyRun matches: <receiver>.apply { <body> } or <receiver>.run { <body> }
func parseApplyRun(stmt string) (receiver, inner string, ok bool) {
	for _, kw := range []string{".apply {", ".run {"} {
		idx := findAtParenDepth0(stmt, kw)
		if idx < 0 {
			continue
		}
		receiver = strings.TrimSpace(stmt[:idx])
		afterBrace := stmt[idx+len(kw):]
		inner = stripTrailingBlock(afterBrace)
		ok = true
		return
	}
	return
}

// parseWith matches: with(<expr>) { <body> }
func parseWith(stmt string) (expr, inner string, ok bool) {
	if !strings.HasPrefix(stmt, "with(") {
		return
	}
	depth := 1
	closeIdx := -1
	for i := 5; i < len(stmt); i++ {
		switch stmt[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				closeIdx = i
			}
		}
		if closeIdx >= 0 {
			break
		}
	}
	if closeIdx < 0 {
		return
	}
	expr = strings.TrimSpace(stmt[5:closeIdx])
	rest := strings.TrimSpace(stmt[closeIdx+1:])
	if !strings.HasPrefix(rest, "{") {
		return
	}
	inner = stripTrailingBlock(rest[1:])
	ok = true
	return
}

// substituteBindings replaces each binding name in s with its bound expression,
// applying bindings in reverse order so later (inner-scope) bindings shadow earlier ones.
func substituteBindings(s string, bindings []depBinding) string {
	for i := len(bindings) - 1; i >= 0; i-- {
		b := bindings[i]
		s = replaceIdentifier(s, b.name, b.expr)
	}
	return s
}

func replaceIdentifier(s, name, replacement string) string {
	if name == "" {
		return s
	}
	runes := []rune(s)
	nameRunes := []rune(name)
	nl := len(nameRunes)
	var result strings.Builder
	i := 0
	for i < len(runes) {
		if i+nl <= len(runes) {
			match := true
			for j := 0; j < nl; j++ {
				if runes[i+j] != nameRunes[j] {
					match = false
					break
				}
			}
			if match {
				prevOk := i == 0 || (!isIdentChar(runes[i-1]) && runes[i-1] != '.')
				nextOk := i+nl >= len(runes) || (!isIdentChar(runes[i+nl]) && runes[i+nl] != '.')
				if prevOk && nextOk {
					result.WriteString(replacement)
					i += nl
					continue
				}
			}
		}
		result.WriteRune(runes[i])
		i++
	}
	return result.String()
}

func isIdentChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func isBareIdentifier(s string) bool {
	return bareIdentRe.MatchString(s)
}

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
	Requests               []DependencyRequest
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

	deps := &Dependencies{}
	blocks, err := extractDependencyBlocks(body)
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return deps, nil
	}
	for _, block := range blocks {
		if err := parseDependencyBlockInto(deps, block); err != nil {
			return nil, err
		}
	}
	return deps, nil
}

func parseDependencyBlockInto(deps *Dependencies, block string) error {
	cleanBlock := stripDependencyComments(block)
	expandedBlock := expandDependencyBindings(cleanBlock)
	for _, match := range dependencyCallRefs(expandedBlock) {
		scope := match[1]
		ref := parseRef(match[2])
		if ref.Kind == "raw" && isBareIdentifier(ref.Value) {
			return fmt.Errorf("unbound reference %q in dependencies block — only top-level vals and let/also/apply/run/with bindings are tracked", ref.Value)
		}
		if deps.Scoped == nil {
			deps.Scoped = map[string][]Ref{}
		}
		deps.Scoped[scope] = append(deps.Scoped[scope], ref)
		deps.Requests = append(deps.Requests, requestFromRef(scope, ref, match[2]))
		addRefByScope(deps, scope, ref)
	}
	return nil
}

func ParseDependenciesForModule(buildFile, rootDir string, pluginIDs []string) (*Dependencies, error) {
	deps, err := ParseDependencies(buildFile)
	if err != nil {
		return nil, err
	}
	for _, pluginFile := range conventionPluginDependencyFiles(rootDir, pluginIDs) {
		pluginDeps, pluginErr := ParseDependencies(pluginFile)
		if pluginErr != nil {
			return nil, pluginErr
		}
		mergeDependencies(deps, pluginDeps)
	}
	// Class-based convention plugins (.kt sources registered via a
	// build-logic gradlePlugin { plugins { register(...) } } block)
	// declare their dependencies imperatively, via `add("scope", expr)`
	// inside a `dependencies { ... }` block. ParseDependencies only
	// understands the script-style property accessors, so a parallel
	// extractor handles the .kt form.
	for _, ktFile := range classConventionPluginFiles(rootDir, pluginIDs) {
		data, ktErr := os.ReadFile(ktFile) // #nosec G304 -- build-logic source under project root
		if ktErr != nil {
			continue
		}
		parseClassConventionDependencies(string(data), deps)
	}
	return deps, nil
}

func conventionPluginDependencyFiles(rootDir string, pluginIDs []string) []string {
	if strings.TrimSpace(rootDir) == "" || len(pluginIDs) == 0 {
		return nil
	}
	pluginSet := map[string]struct{}{}
	for _, id := range pluginIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			pluginSet[id] = struct{}{}
		}
	}
	if len(pluginSet) == 0 {
		return nil
	}
	var out []string
	for _, root := range conventionBuildRoots(rootDir) {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".gradle.kts") && !strings.HasSuffix(name, ".gradle") {
				return nil
			}
			if !strings.Contains(filepath.ToSlash(path), "/src/main/") {
				return nil
			}
			id := strings.TrimSuffix(name, ".gradle.kts")
			id = strings.TrimSuffix(id, ".gradle")
			if _, ok := pluginSet[id]; ok {
				out = append(out, path)
			}
			return nil
		})
	}
	return out
}

func conventionBuildRoots(rootDir string) []string {
	if strings.TrimSpace(rootDir) == "" {
		return nil
	}
	seen := map[string]bool{}
	var roots []string
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		roots = append(roots, path)
	}
	add(filepath.Join(rootDir, "build-logic"))
	for _, settings := range []string{filepath.Join(rootDir, "settings.gradle.kts"), filepath.Join(rootDir, "settings.gradle")} {
		data, err := os.ReadFile(settings) // #nosec
		if err != nil {
			continue
		}
		re := regexp.MustCompile(`includeBuild\s*\(\s*"([^"]+)"\s*\)`)
		for _, match := range re.FindAllStringSubmatch(string(data), -1) {
			if len(match) >= 2 {
				add(filepath.Clean(filepath.Join(rootDir, match[1])))
			}
		}
		break
	}
	return roots
}

func mergeDependencies(dst, src *Dependencies) {
	if dst == nil || src == nil {
		return
	}
	dst.Main = append(dst.Main, src.Main...)
	dst.Debug = append(dst.Debug, src.Debug...)
	dst.Test = append(dst.Test, src.Test...)
	dst.AndroidTest = append(dst.AndroidTest, src.AndroidTest...)
	dst.CompileOnly = append(dst.CompileOnly, src.CompileOnly...)
	dst.RuntimeOnly = append(dst.RuntimeOnly, src.RuntimeOnly...)
	dst.TestCompileOnly = append(dst.TestCompileOnly, src.TestCompileOnly...)
	dst.TestRuntimeOnly = append(dst.TestRuntimeOnly, src.TestRuntimeOnly...)
	dst.AndroidTestCompileOnly = append(dst.AndroidTestCompileOnly, src.AndroidTestCompileOnly...)
	dst.AndroidTestRuntimeOnly = append(dst.AndroidTestRuntimeOnly, src.AndroidTestRuntimeOnly...)
	dst.CoreLibraryDesugaring = append(dst.CoreLibraryDesugaring, src.CoreLibraryDesugaring...)
	dst.Requests = append(dst.Requests, src.Requests...)
	if len(src.Scoped) > 0 && dst.Scoped == nil {
		dst.Scoped = map[string][]Ref{}
	}
	for scope, refs := range src.Scoped {
		dst.Scoped[scope] = append(dst.Scoped[scope], refs...)
	}
}

func dependencyCallRefs(block string) [][]string {
	var out [][]string
	kotlinCall := regexp.MustCompile(`(?m)^\s*([A-Za-z][A-Za-z0-9_]*)\((.+?)\)\s*(?:\{.*\})?\s*$`)
	out = append(out, kotlinCall.FindAllStringSubmatch(block, -1)...)
	groovyCall := regexp.MustCompile(`(?m)^\s*([A-Za-z][A-Za-z0-9_]*)\s+(.+?)\s*(?:\{.*\})?\s*$`)
	out = append(out, groovyCall.FindAllStringSubmatch(block, -1)...)
	return out
}

func addRefByScope(deps *Dependencies, scope string, ref Ref) {
	switch scope {
	case "api", "implementation":
		deps.Main = append(deps.Main, ref)
	case "debugImplementation":
		deps.Debug = append(deps.Debug, ref)
	case "testImplementation", "unitTestImplementation":
		deps.Test = append(deps.Test, ref)
	case "androidTestImplementation":
		deps.AndroidTest = append(deps.AndroidTest, ref)
	case "compileOnly":
		deps.CompileOnly = append(deps.CompileOnly, ref)
	case "runtimeOnly":
		deps.RuntimeOnly = append(deps.RuntimeOnly, ref)
	case "testCompileOnly", "unitTestCompileOnly":
		deps.TestCompileOnly = append(deps.TestCompileOnly, ref)
	case "androidTestCompileOnly":
		deps.AndroidTestCompileOnly = append(deps.AndroidTestCompileOnly, ref)
	case "testRuntimeOnly", "unitTestRuntimeOnly":
		deps.TestRuntimeOnly = append(deps.TestRuntimeOnly, ref)
	case "androidTestRuntimeOnly":
		deps.AndroidTestRuntimeOnly = append(deps.AndroidTestRuntimeOnly, ref)
	case "coreLibraryDesugaring":
		deps.CoreLibraryDesugaring = append(deps.CoreLibraryDesugaring, ref)
	}
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

func extractDependencyBlocks(body string) ([]string, error) {
	var blocks []string
	searchFrom := 0
	for {
		idx := strings.Index(body[searchFrom:], "dependencies {")
		if idx < 0 {
			break
		}
		start := searchFrom + idx
		searchFrom = start + len("dependencies {")
		if !includeDependencyBlock(body, start) {
			continue
		}
		block, end, ok := dependencyBlockAt(body[start:])
		if !ok {
			return nil, fmt.Errorf("unterminated dependencies block")
		}
		blocks = append(blocks, block)
		searchFrom = start + end
	}
	return blocks, nil
}

func includeDependencyBlock(body string, start int) bool {
	prefix := strings.TrimRight(body[:start], " \t\r\n")
	if !strings.HasSuffix(prefix, ".") {
		return true
	}
	prefix = strings.TrimSuffix(prefix, ".")
	i := len(prefix) - 1
	for i >= 0 {
		r := rune(prefix[i])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		i--
	}
	receiver := prefix[i+1:]
	return receiver == "commonMain" || receiver == "androidMain"
}

func dependencyBlockAt(rest string) (string, int, bool) {
	depth := 0
	for i, r := range rest {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[:i+1], i + 1, true
			}
		}
	}
	return "", 0, false
}

// ParseRef parses a single Gradle dependency expression into a Ref.
// It recognizes platform(...), files/fileTree wrappers, libs.bundles.X,
// libs.X, project("..."), projects.foo.bar, and quoted "group:artifact:ver".
func ParseRef(expr string) Ref {
	return parseRef(expr)
}

func parseRef(expr string) Ref {
	expr = strings.TrimSpace(expr)
	expr = stripTrailingClosure(expr)
	if strings.HasPrefix(expr, "enforcedPlatform(") && strings.HasSuffix(expr, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(expr, "enforcedPlatform("), ")")
		ref := parseRef(inner)
		ref.Kind = "enforced-platform-" + ref.Kind
		return ref
	}
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
	case strings.HasPrefix(expr, "project('") && strings.HasSuffix(expr, "')"):
		return Ref{Kind: "project", Value: strings.TrimSuffix(strings.TrimPrefix(expr, "project('"), "')")}
	case strings.HasPrefix(expr, "projects."):
		parts := strings.Split(strings.TrimPrefix(expr, "projects."), ".")
		for i, part := range parts {
			parts[i] = camelToKebab(part)
		}
		return Ref{Kind: "project", Value: ":" + strings.Join(parts, ":")}
	case isQuotedString(expr):
		return Ref{Kind: "raw", Value: strings.Trim(expr, `"'`)}
	default:
		return Ref{Kind: "raw", Value: expr}
	}
}

func requestFromRef(scope string, ref Ref, raw string) DependencyRequest {
	req := DependencyRequest{
		Scope:         scope,
		Kind:          ref.Kind,
		Value:         ref.Value,
		Raw:           strings.TrimSpace(raw),
		Configuration: scope,
	}
	kind := ref.Kind
	if strings.HasPrefix(kind, "enforced-platform-") {
		req.Enforced = true
		req.Platform = true
		kind = strings.TrimPrefix(kind, "enforced-platform-")
	} else if strings.HasPrefix(kind, "platform-") {
		req.Platform = true
		kind = strings.TrimPrefix(kind, "platform-")
	}
	switch kind {
	case "project":
		req.Project = true
	case "library":
		req.CatalogAlias = ref.Value
	case "bundle":
		req.BundleAlias = ref.Value
	}
	return req
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
	return len(expr) >= 2 && ((strings.HasPrefix(expr, `"`) && strings.HasSuffix(expr, `"`)) || (strings.HasPrefix(expr, `'`) && strings.HasSuffix(expr, `'`)))
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
