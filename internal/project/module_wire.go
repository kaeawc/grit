package project

import (
	"path/filepath"
	"regexp"
	"strings"
)

// detectWirePlugin reports whether the module applies the `com.squareup.wire`
// Gradle plugin. It checks the standard `id("com.squareup.wire")` form, the
// alias-via-version-catalog form `alias(libs.plugins.square.wire)`, and the
// shorter `libs.plugins.square.wire` reference.
func detectWirePlugin(body string) bool {
	if strings.Contains(body, `id("com.squareup.wire")`) {
		return true
	}
	if strings.Contains(body, `id 'com.squareup.wire'`) {
		return true
	}
	if strings.Contains(body, "libs.plugins.square.wire") {
		return true
	}
	if strings.Contains(body, "libs.plugins.wire") {
		return true
	}
	return false
}

// parseWireConfig extracts a WireConfig from the body of a build script. When
// the plugin is applied but no `wire { }` block is present, it returns the
// documented defaults. modDir is used to resolve relative srcDir paths.
func parseWireConfig(body, modDir string) WireConfig {
	cfg := WireConfig{}
	block, ok := extractNamedBlock(body, "wire")
	if !ok {
		// Defaults: wire scans src/main/proto when no sourcePath is configured,
		// targets kotlin, with javaInterop off.
		cfg.SourcePaths = []string{filepath.Join(modDir, "src", "main", "proto")}
		cfg.KotlinTarget = true
		return cfg
	}

	cfg.ProtoLibrary = parseAssignment(block, `protoLibrary\s*=\s*(true|false)`) == "true"

	if kotlinBlock, ok := extractNamedBlock(block, "kotlin"); ok {
		cfg.KotlinTarget = true
		cfg.JavaInterop = parseAssignment(kotlinBlock, `javaInterop\s*=\s*(true|false)`) == "true"
	}
	if _, ok := extractNamedBlock(block, "java"); ok {
		cfg.JavaTarget = true
	}
	if !cfg.KotlinTarget && !cfg.JavaTarget {
		// Wire defaults to Kotlin when no target block is declared.
		cfg.KotlinTarget = true
	}

	cfg.SourcePaths = resolveSrcDirs(block, "sourcePath", modDir)
	cfg.ProtoPaths = resolveSrcDirs(block, "protoPath", modDir)
	if len(cfg.SourcePaths) == 0 {
		cfg.SourcePaths = []string{filepath.Join(modDir, "src", "main", "proto")}
	}

	cfg.Includes = parseQuotedListCalls(block, "includes")
	cfg.Excludes = parseQuotedListCalls(block, "excludes")
	return cfg
}

// resolveSrcDirs pulls `srcDir("…")` paths out of a named subblock and joins
// them onto modDir.
func resolveSrcDirs(block, name, modDir string) []string {
	sub, ok := extractNamedBlock(block, name)
	if !ok {
		return nil
	}
	re := regexp.MustCompile(`srcDir\s*\(\s*"([^"]+)"\s*\)`)
	matches := re.FindAllStringSubmatch(sub, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		path := strings.TrimSpace(match[1])
		if path == "" {
			continue
		}
		resolved := path
		if !filepath.IsAbs(path) {
			resolved = filepath.Join(modDir, path)
		}
		if _, dup := seen[resolved]; dup {
			continue
		}
		seen[resolved] = struct{}{}
		out = append(out, resolved)
	}
	return out
}

func parseQuotedListCalls(block, name string) []string {
	re := regexp.MustCompile(name + `\s*=\s*listOf\(([^)]*)\)`)
	match := re.FindStringSubmatch(block)
	if len(match) < 2 {
		return nil
	}
	var out []string
	for _, value := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(match[1], -1) {
		if len(value) >= 2 {
			out = append(out, strings.TrimSpace(value[1]))
		}
	}
	return out
}
