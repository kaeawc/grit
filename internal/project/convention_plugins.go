package project

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kaeawc/grit/internal/modulebuild"
)

// conventionPluginMap walks <rootDir>/build-logic looking for precompiled
// Kotlin DSL convention plugin scripts (foo.gradle.kts under any src/main
// path). Each script's basename (minus .gradle.kts / .gradle) is treated as
// the plugin id, and its plugins{} block is parsed for the plugin ids it
// applies. Returns conventionID -> applied plugin ids.
func conventionPluginMap(rootDir string, pluginAliases map[string]string) map[string][]string {
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
		mergeRegisteredConventions(root, pluginAliases, out)
	}
	return out
}

// mergeRegisteredConventions handles the class-based convention
// plugin pattern: a build-logic sub-project's build.gradle.kts
// declares `gradlePlugin { plugins { register("foo") { id = "X"; implementationClass = "Y" } } }`
// and Y.kt under src/main/kotlin (any subpath) calls
// pluginManager.apply("Z") / apply("Z"). The convention map needs
// "X" → ["Z"...] so a module that aliases X in its plugins{} block
// gets classified as android-application / android-library / etc.
//
// Script-based convention plugins (foo.gradle.kts under src/main)
// stay with the outer walker; this only adds class-based entries.
func mergeRegisteredConventions(buildLogicRoot string, pluginAliases map[string]string, out map[string][]string) {
	var registrations []modulebuild.PluginRegistration
	implIndex := map[string]string{} // class basename -> kotlin source path

	_ = filepath.WalkDir(buildLogicRoot, func(path string, d fs.DirEntry, err error) error {
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
			registrations = append(registrations, modulebuild.ParsePluginRegistrations(string(data))...)
		case strings.HasSuffix(d.Name(), ".kt"):
			implIndex[strings.TrimSuffix(d.Name(), ".kt")] = path
		}
		return nil
	})

	for _, reg := range registrations {
		if reg.ID == "" || reg.ImplClass == "" {
			continue
		}
		// The registration's implementationClass is the fully qualified
		// class name; the impl source file is named for the simple
		// class name (the last `.`-separated segment). Match on the
		// simple name so packages don't matter.
		src, ok := implIndex[modulebuild.SimpleClassName(reg.ImplClass)]
		if !ok {
			continue
		}
		data, err := os.ReadFile(src) // #nosec G304 -- impl class file under project root
		if err != nil {
			continue
		}
		applied := parseAppliedPluginIDs(string(data), pluginAliases)
		if len(applied) == 0 {
			continue
		}
		out[reg.ID] = mergeStrings(out[reg.ID], applied)
	}
}

// appliedPluginRe matches the `apply("plugin.id")` form that
// class-based convention plugins typically use inside a
// `with(pluginManager) { ... }` or `pluginManager.apply(...)` block.
var appliedPluginRe = regexp.MustCompile(`apply\s*\(\s*"([^"]+)"\s*\)`)

// appliedAccessorRe matches `apply(<accessor>.pluginId)` forms.
// Class-based convention plugins in many real projects look up plugin
// ids through the version-catalog accessor — e.g.
// `target.plugins.apply(target.libs.plugins.kotlin.jvm.pluginId)`.
// The accessor is captured as a dotted path; we then look up the
// pluginAliases map to resolve "libs.plugins.kotlin.jvm.pluginId"
// (or trailing segments thereof) back to an "org.jetbrains.kotlin.jvm"
// plugin id.
var appliedAccessorRe = regexp.MustCompile(`apply\s*\(\s*([A-Za-z_][A-Za-z0-9_.]*)\.pluginId\s*\)`)

func parseAppliedPluginIDs(body string, pluginAliases map[string]string) []string {
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

	for _, m := range appliedPluginRe.FindAllStringSubmatch(body, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	if len(pluginAliases) > 0 {
		for _, m := range appliedAccessorRe.FindAllStringSubmatch(body, -1) {
			if len(m) < 2 {
				continue
			}
			if id := resolvePluginAccessor(m[1], pluginAliases); id != "" {
				add(id)
			}
		}
	}
	return out
}

// resolvePluginAccessor maps a dotted accessor expression like
// "target.libs.plugins.kotlin.jvm" or "libs.plugins.kotlin.jvm" to
// the underlying plugin id by stripping the receiver prefix
// (everything up to and including "plugins.") and looking up the
// remaining alias in pluginAliases (keyed in dot form — the same
// shape loadVersionCatalogPluginAliases produces).
func resolvePluginAccessor(accessor string, pluginAliases map[string]string) string {
	segments := strings.Split(accessor, ".")
	pluginsIdx := -1
	for i, seg := range segments {
		if seg == "plugins" {
			pluginsIdx = i
			break
		}
	}
	if pluginsIdx < 0 || pluginsIdx == len(segments)-1 {
		return ""
	}
	alias := strings.Join(segments[pluginsIdx+1:], ".")
	if id := pluginAliases[alias]; id != "" {
		return id
	}
	return ""
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
