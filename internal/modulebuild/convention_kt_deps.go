package modulebuild

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// classConventionPluginFiles returns the Kotlin source paths of class-based
// convention plugins whose registered plugin id is in pluginIDs. The
// registration comes from a build-logic sub-project's build.gradle.kts
// gradlePlugin { plugins { register(...) } } block; the implementationClass
// is matched against .kt file basenames under build-logic so any package
// layout is accepted.
func classConventionPluginFiles(rootDir string, pluginIDs []string) []string {
	if strings.TrimSpace(rootDir) == "" || len(pluginIDs) == 0 {
		return nil
	}
	wantedIDs := map[string]struct{}{}
	for _, id := range pluginIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			wantedIDs[id] = struct{}{}
		}
	}
	if len(wantedIDs) == 0 {
		return nil
	}

	var out []string
	for _, root := range conventionBuildRoots(rootDir) {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		var registrations []PluginRegistration
		implIndex := map[string]string{} // simple class name -> .kt source path
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			switch {
			case strings.HasSuffix(d.Name(), "build.gradle.kts"), strings.HasSuffix(d.Name(), "build.gradle"):
				// #nosec G304 G122 -- build-logic file under the project's
				// own root; walker visits each entry once, no symlink chase.
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					return nil
				}
				registrations = append(registrations, ParsePluginRegistrations(string(data))...)
			case strings.HasSuffix(d.Name(), ".kt"):
				implIndex[strings.TrimSuffix(d.Name(), ".kt")] = path
			}
			return nil
		})
		for _, reg := range registrations {
			if _, wanted := wantedIDs[reg.ID]; !wanted {
				continue
			}
			if src, ok := implIndex[SimpleClassName(reg.ImplClass)]; ok {
				out = append(out, src)
			}
		}
	}
	return out
}

// parseClassConventionDependencies scans the body of a class-based
// convention plugin .kt source for `dependencies { add("scope", expr) }`
// statements and merges the resulting Refs into deps. Unlike script-style
// dependency blocks, the .kt form is always `add("config", <expr>)`: the
// scope is a quoted literal and the expression resolves catalog accessors
// through `libs.findLibrary("alias").get()` / `libs.findBundle("alias")
// .get()` instead of the property-style `libs.alias`.
//
// Notes:
//   - The extractor is balanced-paren rather than single-line so multi-line
//     `add(\n  "implementation",\n  libs.findLibrary(...).get()\n)` works.
//   - All add(...) calls in the body are extracted, including those inside
//     `pluginManager.withPlugin(...) { ... }` conditionals. The conditional
//     gate is not honored yet — every contribution flows through
//     unconditionally. PR B is the place to evaluate the gates.
//   - "ksp" / "kapt" scopes land in deps.Scoped only. Callers wiring KSP
//     processor classpaths should additionally call
//     ParseConventionKSPProcessors, which targets the same add(...) form
//     but emits *just* the ksp-family scope contributions ready to merge
//     into mod.KSP.Processors.
func parseClassConventionDependencies(body string, deps *Dependencies) {
	for _, block := range extractKotlinDependencyBlocks(body) {
		clean := stripDependencyComments(block)
		for _, call := range extractKotlinAddCalls(clean) {
			ref := parseClassDependencyExpr(call.expr)
			if ref.Kind == "" || (ref.Kind == "raw" && ref.Value == "") {
				continue
			}
			// Raw bare identifiers (e.g. `add("implementation", platform(bom))`
			// where `bom = libs.findLibrary("firebase-bom").get()` is a local
			// val) are local Kotlin variable references, not Maven coordinates.
			// Skip them rather than feeding garbage to the downstream resolver.
			// Properly tracking local val bindings inside .kt convention bodies
			// is a TODO for a follow-up.
			if isLocalIdentifierRef(ref) {
				continue
			}
			if deps.Scoped == nil {
				deps.Scoped = map[string][]Ref{}
			}
			deps.Scoped[call.scope] = append(deps.Scoped[call.scope], ref)
			deps.Requests = append(deps.Requests, requestFromRef(call.scope, ref, call.expr))
			addRefByScope(deps, call.scope, ref)
		}
	}
}

// ParseConventionKSPProcessors returns the KSP processor Refs declared
// via `add("ksp...", expr)` inside class-based convention plugin sources
// applied by the given module (identified by its post-expansion plugin
// id list). Used to plumb convention-plugin-contributed KSP processors
// into mod.KSP.Processors so the processor classpath gets the right
// jars even when the module itself never names a processor.
//
// Scope names are matched as "ksp" or "kspX" (e.g. "kspAndroidTest"),
// matching the existing kspAddDepRE in internal/project/module_ksp.go.
func ParseConventionKSPProcessors(rootDir string, pluginIDs []string) []Ref {
	files := classConventionPluginFiles(rootDir, pluginIDs)
	if len(files) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []Ref
	for _, path := range files {
		data, err := os.ReadFile(path) // #nosec G304 -- build-logic source under project root
		if err != nil {
			continue
		}
		for _, block := range extractKotlinDependencyBlocks(string(data)) {
			clean := stripDependencyComments(block)
			for _, call := range extractKotlinAddCalls(clean) {
				if !isKSPScope(call.scope) {
					continue
				}
				ref := parseClassDependencyExpr(call.expr)
				if ref.Kind == "" || ref.Value == "" || isLocalIdentifierRef(ref) {
					continue
				}
				out = AppendUniqueRef(out, ref, seen)
			}
		}
	}
	return out
}

// AppendUniqueRef adds ref to out only if (Kind, Value) isn't already
// present, tracking presence via seen so callers can interleave the
// same dedup state across multiple appends. seen may be nil-initialized
// from a fresh make; the function never returns the input map.
func AppendUniqueRef(out []Ref, ref Ref, seen map[string]struct{}) []Ref {
	key := ref.Kind + "|" + ref.Value
	if _, dup := seen[key]; dup {
		return out
	}
	seen[key] = struct{}{}
	return append(out, ref)
}

// isLocalIdentifierRef reports whether ref refers to a local Kotlin
// identifier (i.e. a `val name = ...` binding) rather than a Maven
// coordinate. Such refs slip through when convention plugins bind a
// catalog accessor to a val and then pass the val to add(...), e.g.
// `val bom = libs.findLibrary("X").get(); add("implementation", platform(bom))`.
// The downstream resolver rejects bare-identifier raw refs, so it's
// safer to drop them here than to feed garbage forward. Properly
// tracking local val bindings inside class-based convention bodies is
// follow-up work.
//
// The HasSuffix check covers the three "raw" kinds parseClassDependencyExpr
// can emit: "raw", "platform-raw", and "enforced-platform-raw".
func isLocalIdentifierRef(ref Ref) bool {
	return strings.HasSuffix(ref.Kind, "raw") && ref.Value != "" && isBareIdentifier(ref.Value)
}

// isKSPScope reports whether the configuration name is "ksp" or a
// variant-specific KSP configuration (e.g. "kspAndroidTest", "kspDebug").
func isKSPScope(scope string) bool {
	if scope == "ksp" {
		return true
	}
	if !strings.HasPrefix(scope, "ksp") || len(scope) == 3 {
		return false
	}
	c := scope[3]
	return c >= 'A' && c <= 'Z'
}

type kotlinAddCall struct {
	scope string
	expr  string
}

// extractKotlinAddCalls finds every `add("scope", expr)` call in body,
// tolerating arbitrary whitespace, newlines, and nested parens inside expr.
func extractKotlinAddCalls(body string) []kotlinAddCall {
	var out []kotlinAddCall
	i := 0
	for i < len(body) {
		// Locate the next "add" identifier followed by "(".
		idx := strings.Index(body[i:], "add")
		if idx < 0 {
			break
		}
		start := i + idx
		// "add" must be a standalone identifier (not the tail of e.g. "head").
		if start > 0 && isIdentChar(rune(body[start-1])) {
			i = start + 1
			continue
		}
		j := start + len("add")
		for j < len(body) && (body[j] == ' ' || body[j] == '\t') {
			j++
		}
		if j >= len(body) || body[j] != '(' {
			i = start + 1
			continue
		}
		args, end, ok := readBalancedArgs(body[j:])
		if !ok {
			break
		}
		scope, expr, split := splitAddArgs(args)
		if split {
			out = append(out, kotlinAddCall{scope: scope, expr: expr})
		}
		i = j + end
	}
	return out
}

// readBalancedArgs reads from "(" through the matching ")" and returns
// the content between them (without the parens) plus the offset just past
// the closing paren.
func readBalancedArgs(s string) (string, int, bool) {
	if len(s) == 0 || s[0] != '(' {
		return "", 0, false
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i], i + 1, true
			}
		}
	}
	return "", 0, false
}

// splitAddArgs splits an `add("scope", expr)` argument list into the scope
// (the quoted first arg) and the expression (the rest, trimmed). Returns
// false if the args don't have the expected shape.
func splitAddArgs(args string) (string, string, bool) {
	args = strings.TrimSpace(args)
	if !strings.HasPrefix(args, "\"") {
		return "", "", false
	}
	closeQuote := strings.IndexByte(args[1:], '"')
	if closeQuote < 0 {
		return "", "", false
	}
	scope := args[1 : 1+closeQuote]
	rest := strings.TrimSpace(args[1+closeQuote+1:])
	if !strings.HasPrefix(rest, ",") {
		return "", "", false
	}
	expr := strings.TrimSpace(rest[1:])
	// Multi-line add(...) calls often end with a trailing comma per
	// Kotlin style; drop it so the expression matches stripFindLibraryCall's
	// anchored regex.
	expr = strings.TrimRight(expr, ",")
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", "", false
	}
	return scope, expr, true
}

// extractKotlinDependencyBlocks returns the bodies of every
// `dependencies { ... }` block in a class-based convention plugin source.
// Block extraction is brace-balanced so nested blocks inside the
// dependencies body are tolerated.
func extractKotlinDependencyBlocks(body string) []string {
	var blocks []string
	searchFrom := 0
	for {
		idx := strings.Index(body[searchFrom:], "dependencies {")
		if idx < 0 {
			break
		}
		start := searchFrom + idx
		block, end, ok := dependencyBlockAt(body[start:])
		if !ok {
			break
		}
		blocks = append(blocks, block)
		searchFrom = start + end
	}
	return blocks
}

// parseClassDependencyExpr translates a single `.kt`-form dependency
// expression into a Ref. It mirrors parseRef but understands the imperative
// catalog accessor (`libs.findLibrary("X").get()` / `libs.findBundle("X")
// .get()`) and the platform/enforcedPlatform wrappers.
func parseClassDependencyExpr(expr string) Ref {
	expr = strings.TrimSpace(expr)
	expr = stripTrailingClosure(expr)

	if inner, ok := unwrapCall(expr, "enforcedPlatform"); ok {
		ref := parseClassDependencyExpr(inner)
		ref.Kind = "enforced-platform-" + ref.Kind
		return ref
	}
	if inner, ok := unwrapCall(expr, "platform"); ok {
		ref := parseClassDependencyExpr(inner)
		ref.Kind = "platform-" + ref.Kind
		return ref
	}
	if inner, ok := stripFindLibraryCall(expr); ok {
		return Ref{Kind: "library", Value: inner}
	}
	if inner, ok := stripFindBundleCall(expr); ok {
		return Ref{Kind: "bundle", Value: inner}
	}
	if module, ok := stripKotlinModuleCall(expr); ok {
		return Ref{Kind: "raw", Value: "org.jetbrains.kotlin:kotlin-" + module}
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
	case isQuotedString(expr):
		return Ref{Kind: "raw", Value: strings.Trim(expr, `"'`)}
	}
	return Ref{Kind: "raw", Value: expr}
}

var (
	findLibraryRe  = regexp.MustCompile(`^libs\.findLibrary\s*\(\s*"([^"]+)"\s*\)(?:\.(?:get|orNull|orElse[A-Za-z]*)\s*\([^)]*\))?$`)
	findBundleRe   = regexp.MustCompile(`^libs\.findBundle\s*\(\s*"([^"]+)"\s*\)(?:\.(?:get|orNull|orElse[A-Za-z]*)\s*\([^)]*\))?$`)
	kotlinModuleRe = regexp.MustCompile(`^kotlin\s*\(\s*"([^"]+)"\s*\)$`)
)

func stripFindLibraryCall(expr string) (string, bool) {
	match := findLibraryRe.FindStringSubmatch(strings.TrimSpace(expr))
	if len(match) < 2 {
		return "", false
	}
	return match[1], true
}

func stripFindBundleCall(expr string) (string, bool) {
	match := findBundleRe.FindStringSubmatch(strings.TrimSpace(expr))
	if len(match) < 2 {
		return "", false
	}
	return match[1], true
}

// stripKotlinModuleCall recognizes the Gradle Kotlin DSL shorthand
// `kotlin("X")`, which expands to `org.jetbrains.kotlin:kotlin-X`. The
// version is left unset so downstream catalog/Kotlin-toolchain alignment
// (see internal/m2local/platforms.go inferAlignedVersion) supplies it.
func stripKotlinModuleCall(expr string) (string, bool) {
	match := kotlinModuleRe.FindStringSubmatch(strings.TrimSpace(expr))
	if len(match) < 2 {
		return "", false
	}
	return match[1], true
}
