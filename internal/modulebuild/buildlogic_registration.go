package modulebuild

import (
	"regexp"
	"strings"
)

// PluginRegistration is a single (id, implementationClass) pair from a
// build-logic sub-project's `gradlePlugin { plugins { register(...) } }`
// block. Callers use it to map a registered plugin id back to its
// Kotlin source file on disk so the source can be parsed for applied
// plugin ids or imperative dependency contributions.
type PluginRegistration struct {
	ID        string
	ImplClass string
}

var (
	pluginRegisterRe = regexp.MustCompile(`register\s*\(\s*"([^"]*)"\s*\)\s*\{([^}]*)\}`)
	pluginIDFieldRe  = regexp.MustCompile(`(?m)\s*id\s*=\s*"([^"]+)"`)
	pluginImplRe     = regexp.MustCompile(`(?m)\s*implementationClass\s*=\s*"([^"]+)"`)
)

// ParsePluginRegistrations extracts every register("name") { id = X;
// implementationClass = Y } entry from body. id/implementationClass may
// appear in either order inside the inner block.
func ParsePluginRegistrations(body string) []PluginRegistration {
	var out []PluginRegistration
	for _, match := range pluginRegisterRe.FindAllStringSubmatch(body, -1) {
		if len(match) < 3 {
			continue
		}
		idMatch := pluginIDFieldRe.FindStringSubmatch(match[2])
		implMatch := pluginImplRe.FindStringSubmatch(match[2])
		if len(idMatch) < 2 || len(implMatch) < 2 {
			continue
		}
		out = append(out, PluginRegistration{ID: idMatch[1], ImplClass: implMatch[1]})
	}
	return out
}

// SimpleClassName returns the last `.`-separated segment of a dotted
// class name, e.g. "com.example.Foo" -> "Foo". Used to match a
// registered implementationClass to its .kt source file, since the
// file is named for the simple class name regardless of package.
func SimpleClassName(fqcn string) string {
	if idx := strings.LastIndex(fqcn, "."); idx >= 0 {
		return fqcn[idx+1:]
	}
	return fqcn
}
