package project

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func parseRepeatedAssignments(body string, pattern string) []string {
	re := regexp.MustCompile(pattern)
	matches := re.FindAllStringSubmatch(body, -1)
	var out []string
	for _, match := range matches {
		if len(match) > 1 {
			out = append(out, match[1])
		}
	}
	return out
}

func parseAssignment(body string, pattern string) string {
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(body)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func parseOptionalBool(body string, pattern string) (*bool, bool) {
	value := parseAssignment(body, pattern)
	if value == "" {
		return nil, false
	}
	parsed := value == "true"
	return &parsed, true
}

func braceDepth(body string) int {
	return strings.Count(body, "{") - strings.Count(body, "}")
}

func parseCatalogRef(body string, pattern string) string {
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(body)
	if len(match) > 1 {
		return "catalog:" + strings.ReplaceAll(match[1], ".", "-")
	}
	return ""
}

func countFiles(root string, include func(path string) bool) int {
	count := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if include(path) {
			count++
		}
		return nil
	})
	return count
}

// discoverAidlFiles scans src/{sourceSet}/aidl/ directories under modDir
// for .aidl files and returns their paths relative to modDir.
func discoverAidlFiles(modDir string, sourceSets []string) []string {
	var files []string
	for _, ss := range sourceSets {
		aidlDir := filepath.Join(modDir, "src", ss, "aidl")
		_ = filepath.WalkDir(aidlDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, ".aidl") {
				rel, relErr := filepath.Rel(modDir, path)
				if relErr == nil {
					files = append(files, rel)
				}
			}
			return nil
		})
	}
	return files
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func firstExisting(paths ...string) string {
	for _, path := range paths {
		if fileExists(path) {
			return path
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func extractNamedBlock(body, name string) (string, bool) {
	idx := strings.Index(body, name+" {")
	if idx < 0 {
		return "", false
	}
	openIdx := idx + len(name)
	block, _, ok := extractBraceBodyAt(body, openIdx)
	return block, ok
}

func extractBraceBodyAt(body string, openIdx int) (string, int, bool) {
	start := strings.Index(body[openIdx:], "{")
	if start < 0 {
		return "", 0, false
	}
	start += openIdx
	depth := 0
	for i := start; i < len(body); i++ {
		switch body[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return body[start+1 : i], i, true
			}
		}
	}
	return "", 0, false
}

func captureSubmatch(body string, indexes []int, submatch int) string {
	if len(indexes) <= submatch+1 {
		return ""
	}
	start := indexes[submatch]
	end := indexes[submatch+1]
	if start < 0 || end < 0 {
		return ""
	}
	return body[start:end]
}

func collectPluginAliases(body string) []string {
	re := regexp.MustCompile(`alias\(libs\.plugins\.([A-Za-z0-9\.\-_]+)\)`)
	matches := re.FindAllStringSubmatch(body, -1)
	seen := map[string]bool{}
	var out []string
	for _, match := range matches {
		name := match[1]
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sortStrings(out)
	return out
}

func sortStrings(values []string) {
	if len(values) < 2 {
		return
	}
	for i := 0; i < len(values)-1; i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}

func resolveRepositoryURLExpr(expr string, gradleProperties map[string]string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return ""
	}
	if value := parseAssignment(expr, `"((?:https?|file)://[^"]+)"`); value != "" {
		return value
	}
	if key := parseAssignment(expr, `findProperty\("([^"]+)"\)`); key != "" {
		return strings.TrimSpace(gradleProperties[key])
	}
	return ""
}

func parseProjectDirAssignments(body string) map[string]string {
	assignments := map[string]string{}
	re := regexp.MustCompile(`project\("([^"]+)"\)\.projectDir\s*=\s*file\("([^"]+)"\)`)
	for _, match := range re.FindAllStringSubmatch(body, -1) {
		if len(match) < 3 {
			continue
		}
		assignments[strings.TrimSpace(match[1])] = strings.TrimSpace(match[2])
	}
	return assignments
}
