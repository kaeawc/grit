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
// require the line to live inside `dependencies { ... }` because convention-style
// build scripts apply KSP at the top level of the dependencies block and the
// regex anchors to lines starting with `ksp(`.
var kspDepRE = regexp.MustCompile(`(?m)^\s*ksp\(([^()]*(?:\([^()]*\))?[^()]*)\)\s*$`)
var kspAddDepRE = regexp.MustCompile(`(?m)^\s*add\s*\(\s*"ksp[A-Za-z0-9_]*"\s*,\s*([^()]*(?:\([^()]*\))?[^()]*)\)\s*$`)

// parseKSPProcessors extracts processor refs declared as `ksp(...)` lines.
// Each ref is parsed via modulebuild.ParseRef so library aliases, project
// refs, and raw "group:artifact:version" strings all flow through the same
// resolver later.
func parseKSPProcessors(body string) []modulebuild.Ref {
	seen := map[string]struct{}{}
	var out []modulebuild.Ref
	for _, m := range kspDepRE.FindAllStringSubmatch(body, -1) {
		addKSPProcessorRef(&out, seen, m[1])
	}
	for _, m := range kspAddDepRE.FindAllStringSubmatch(body, -1) {
		addKSPProcessorRef(&out, seen, m[1])
	}
	return out
}

func addKSPProcessorRef(out *[]modulebuild.Ref, seen map[string]struct{}, expr string) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return
	}
	ref := modulebuild.ParseRef(expr)
	if ref.Value == "" {
		return
	}
	key := ref.Kind + "|" + ref.Value
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*out = append(*out, ref)
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

// mergeKSPProcessorRefs appends extra processor refs that aren't already
// present in existing, preserving the original ordering. Dedup keys on
// (Kind, Value).
func mergeKSPProcessorRefs(existing, extra []modulebuild.Ref) []modulebuild.Ref {
	if len(extra) == 0 {
		return existing
	}
	seen := map[string]struct{}{}
	for _, ref := range existing {
		seen[ref.Kind+"|"+ref.Value] = struct{}{}
	}
	out := existing
	for _, ref := range extra {
		out = modulebuild.AppendUniqueRef(out, ref, seen)
	}
	return out
}
