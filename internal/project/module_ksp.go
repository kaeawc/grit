package project

import (
	"regexp"
	"strings"

	"github.com/kaeawc/grit/internal/modulebuild"
)

// detectKSPApplied reports whether a build script applies the KSP Gradle
// plugin. It accepts the canonical id-form, the legacy single-quoted Groovy
// form, and the version-catalog `libs.plugins.ksp` alias.
func detectKSPApplied(body string) bool {
	switch {
	case strings.Contains(body, `id("com.google.devtools.ksp")`):
		return true
	case strings.Contains(body, `id 'com.google.devtools.ksp'`):
		return true
	case strings.Contains(body, "libs.plugins.ksp"):
		return true
	}
	return false
}

// kspDepRE matches `ksp(<expr>)` calls inside a dependencies block. We don't
// require the line to live inside `dependencies { ... }` because Signal-style
// build scripts apply KSP at the top level of the dependencies block and the
// regex anchors to lines starting with `ksp(`.
var kspDepRE = regexp.MustCompile(`(?m)^\s*ksp\(([^()]*(?:\([^()]*\))?[^()]*)\)\s*$`)

// parseKSPProcessors extracts processor refs declared as `ksp(...)` lines.
// Each ref is parsed via modulebuild.ParseRef so library aliases, project
// refs, and raw "group:artifact:version" strings all flow through the same
// resolver later.
func parseKSPProcessors(body string) []modulebuild.Ref {
	matches := kspDepRE.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []modulebuild.Ref
	for _, m := range matches {
		expr := strings.TrimSpace(m[1])
		if expr == "" {
			continue
		}
		ref := modulebuild.ParseRef(expr)
		if ref.Value == "" {
			continue
		}
		key := ref.Kind + "|" + ref.Value
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

var kspArgRE = regexp.MustCompile(`(?m)arg\s*\(\s*"([^"]+)"\s*,\s*"([^"]*)"\s*\)`)

// parseKSPOptions extracts processor options from a `ksp { arg("k","v") }`
// block. Returns nil if the block is absent or empty.
func parseKSPOptions(body string) map[string]string {
	block, ok := extractNamedBlock(body, "ksp")
	if !ok {
		return nil
	}
	matches := kspArgRE.FindAllStringSubmatch(block, -1)
	if len(matches) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, m := range matches {
		out[m[1]] = m[2]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseKSPConfig(body string) modulebuild.KSPConfig {
	return modulebuild.KSPConfig{
		Processors: parseKSPProcessors(body),
		Options:    parseKSPOptions(body),
	}
}
