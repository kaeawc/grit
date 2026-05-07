package project

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// conventionPluginMap walks <rootDir>/build-logic looking for precompiled
// Kotlin DSL convention plugin scripts (foo.gradle.kts under any src/main
// path). Each script's basename (minus .gradle.kts / .gradle) is treated as
// the plugin id, and its plugins{} block is parsed for the plugin ids it
// applies. Returns conventionID -> applied plugin ids.
func conventionPluginMap(rootDir string) map[string][]string {
	out := map[string][]string{}
	for _, root := range conventionBuildRoots(rootDir) {
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".gradle.kts") && !strings.HasSuffix(name, ".gradle") {
				return nil
			}
			// Only treat scripts under a src/main/... directory as precompiled
			// convention plugins; skip top-level build scripts of build-logic
			// modules themselves.
			if !strings.Contains(filepath.ToSlash(path), "/src/main/") {
				return nil
			}
			id := strings.TrimSuffix(name, ".gradle.kts")
			id = strings.TrimSuffix(id, ".gradle")
			data, readErr := os.ReadFile(path) // #nosec
			if readErr != nil {
				return nil
			}
			out[id] = collectPluginIDs(string(data))
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
	settings := firstExisting(filepath.Join(rootDir, "settings.gradle.kts"), filepath.Join(rootDir, "settings.gradle"))
	if settings == "" {
		return roots
	}
	data, err := os.ReadFile(settings) // #nosec
	if err != nil {
		return roots
	}
	re := regexp.MustCompile(`includeBuild\s*\(\s*"([^"]+)"\s*\)`)
	for _, match := range re.FindAllStringSubmatch(string(data), -1) {
		if len(match) < 2 {
			continue
		}
		add(filepath.Clean(filepath.Join(rootDir, match[1])))
	}
	return roots
}

// expandPlugins expands any convention-plugin ids in pluginIDs to also
// include the plugin ids that each convention plugin applies. The original
// ids are preserved. The returned list is deduplicated and sorted.
func expandPlugins(pluginIDs []string, conventions map[string][]string) []string {
	if len(pluginIDs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
	}
	var visit func(id string, depth int)
	visit = func(id string, depth int) {
		if depth > 8 {
			return
		}
		add(id)
		for _, applied := range conventions[id] {
			visit(applied, depth+1)
		}
	}
	for _, id := range pluginIDs {
		visit(id, 0)
	}
	sortStrings(out)
	return out
}

func expandPluginAliases(pluginIDs []string, aliases map[string]string) []string {
	if len(pluginIDs) == 0 || len(aliases) == 0 {
		return pluginIDs
	}
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range pluginIDs {
		add(id)
		if canonical := aliases[normalizePluginAlias(id)]; canonical != "" {
			add(canonical)
		}
	}
	sortStrings(out)
	return out
}
