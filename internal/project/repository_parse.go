package project

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/mavenlocalroot"
	"github.com/kaeawc/grit/internal/pathutil"
)

func loadGradleProperties(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out
}

func collectVersionCatalogs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		out = append(out, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(out)
	return out
}

func pickPrimaryCatalog(paths []string) string {
	for _, path := range paths {
		if filepath.Base(path) == "libs.versions.toml" {
			return path
		}
	}
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func collectProjectRepositoriesWithOrigin(body string, gradleProperties map[string]string, origin string) []Repository {
	var repos []Repository
	repositoriesRe := regexp.MustCompile(`(?ms)repositories\s*\{`)
	for _, idx := range repositoriesRe.FindAllStringIndex(body, -1) {
		openIdx := idx[1] - 1
		block, _, ok := extractBraceBodyAt(body, openIdx)
		if !ok {
			continue
		}
		repos = append(repos, parseRepositoriesBlock(block, "dependency", gradleProperties)...)
	}
	return annotateRepositories(dedupeRepositories(repos), origin, 0)
}

func parseRepositoriesBlock(block string, scope string, gradleProperties map[string]string) []Repository {
	var repos []Repository
	trimmed := block
	exclusiveRe := regexp.MustCompile(`(?ms)exclusiveContent\s*\{`)
	for _, idx := range exclusiveRe.FindAllStringIndex(block, -1) {
		openIdx := idx[1] - 1
		exclusiveBody, _, ok := extractBraceBodyAt(block, openIdx)
		if !ok {
			continue
		}
		if repo, ok := parseExclusiveRepository(exclusiveBody, scope, gradleProperties); ok {
			repos = append(repos, repo)
		}
	}
	trimmed = exclusiveRe.ReplaceAllString(trimmed, "")

	simpleCalls := []struct {
		pattern string
		repo    Repository
	}{
		{pattern: `(?m)^\s*google\(\)\s*$`, repo: Repository{Name: "google", Kind: "google", URL: "https://dl.google.com/dl/android/maven2/", Scope: scope}},
		{pattern: `(?m)^\s*mavenCentral\(\)\s*$`, repo: Repository{Name: "mavenCentral", Kind: "mavenCentral", URL: "https://repo1.maven.org/maven2/", Scope: scope}},
		{pattern: `(?m)^\s*gradlePluginPortal\(\)\s*$`, repo: Repository{Name: "gradlePluginPortal", Kind: "gradlePluginPortal", URL: "https://plugins.gradle.org/m2/", Scope: scope}},
		{pattern: `(?m)^\s*jcenter\(\)\s*$`, repo: Repository{Name: "jcenter", Kind: "jcenter", URL: "https://jcenter.bintray.com/", Scope: scope}},
		{pattern: `(?m)^\s*mavenLocal\(\)\s*$`, repo: namedRepository("mavenLocal", scope)},
	}
	for _, candidate := range simpleCalls {
		if regexp.MustCompile(candidate.pattern).FindStringIndex(trimmed) != nil {
			repos = append(repos, candidate.repo)
		}
	}

	blockCalls := regexp.MustCompile(`(?ms)(google|mavenCentral|gradlePluginPortal|jcenter|mavenLocal)\s*\{`)
	for _, idx := range blockCalls.FindAllStringSubmatchIndex(block, -1) {
		name := captureSubmatch(block, idx, 2)
		openIdx := idx[1] - 1
		repoBody, _, ok := extractBraceBodyAt(block, openIdx)
		if !ok {
			continue
		}
		repo := namedRepository(name, scope)
		repo = applyRepositoryBody(repo, repoBody, gradleProperties)
		repos = append(repos, repo)
	}

	mavenCalls := regexp.MustCompile(`(?ms)maven\(\s*"([^"]+)"\s*\)\s*(\{)?|maven\(\s*url\s*=\s*uri\("([^"]+)"\)\s*\)\s*(\{)?|maven\(\s*url\s*=\s*"([^"]+)"\s*\)\s*(\{)?|maven\(\s*findProperty\("([^"]+)"\)!?\!?\s*\)\s*(\{)?`)
	for _, idx := range mavenCalls.FindAllStringSubmatchIndex(block, -1) {
		url := captureSubmatch(block, idx, 2)
		if url == "" {
			url = captureSubmatch(block, idx, 6)
		}
		if url == "" {
			url = captureSubmatch(block, idx, 10)
		}
		if url == "" {
			if key := captureSubmatch(block, idx, 14); key != "" {
				url = strings.TrimSpace(gradleProperties[key])
			}
		}
		if url == "" {
			continue
		}
		repo := Repository{Name: url, Kind: "maven", URL: pathutil.EnsureTrailingSlash(url), Scope: scope}
		openIdx := idx[1] - 1
		if openIdx >= 0 && openIdx < len(block) && block[openIdx] == '{' {
			repoBody, _, ok := extractBraceBodyAt(block, openIdx)
			if ok {
				repo = applyRepositoryBody(repo, repoBody, gradleProperties)
			}
		}
		repos = append(repos, repo)
	}

	mavenBlockCalls := regexp.MustCompile(`(?ms)maven\s*\{`)
	for _, idx := range mavenBlockCalls.FindAllStringIndex(block, -1) {
		openIdx := idx[1] - 1
		repoBody, _, ok := extractBraceBodyAt(block, openIdx)
		if !ok {
			continue
		}
		url := resolveRepositoryURLExpr(repoBody, gradleProperties)
		if url == "" {
			continue
		}
		repo := Repository{Name: url, Kind: "maven", URL: pathutil.EnsureTrailingSlash(url), Scope: scope}
		repo = applyRepositoryBody(repo, repoBody, gradleProperties)
		repos = append(repos, repo)
	}

	return dedupeRepositories(repos)
}

func parseExclusiveRepository(block, scope string, gradleProperties map[string]string) (Repository, bool) {
	forRepoBody, ok := extractNamedBlock(block, "forRepository")
	if !ok {
		return Repository{}, false
	}
	filterBody, ok := extractNamedBlock(block, "filter")
	if !ok {
		return Repository{}, false
	}
	repos := parseRepositoriesBlock(forRepoBody, scope, gradleProperties)
	if len(repos) == 0 {
		return Repository{}, false
	}
	repo := repos[0]
	repo.Exclusive = true
	return applyFilterBody(repo, filterBody), true
}

func namedRepository(name, scope string) Repository {
	switch name {
	case "google":
		return Repository{Name: "google", Kind: "google", URL: "https://dl.google.com/dl/android/maven2/", Scope: scope}
	case "mavenCentral":
		return Repository{Name: "mavenCentral", Kind: "mavenCentral", URL: "https://repo1.maven.org/maven2/", Scope: scope}
	case "gradlePluginPortal":
		return Repository{Name: "gradlePluginPortal", Kind: "gradlePluginPortal", URL: "https://plugins.gradle.org/m2/", Scope: scope}
	case "jcenter":
		return Repository{Name: "jcenter", Kind: "jcenter", URL: "https://jcenter.bintray.com/", Scope: scope}
	case "mavenLocal":
		repo := Repository{Name: "mavenLocal", Kind: "mavenLocal", Scope: scope}
		if root := mavenlocalroot.Default(); root != "" {
			repo.URL = "file://" + filepath.ToSlash(root)
		}
		return repo
	default:
		return Repository{Name: name, Kind: name, Scope: scope}
	}
}

func applyRepositoryBody(repo Repository, body string, gradleProperties map[string]string) Repository {
	if name := parseAssignment(body, `name\s*=\s*"([^"]+)"`); name != "" {
		repo.Name = name
	}
	if url := resolveRepositoryURLExpr(body, gradleProperties); url != "" {
		repo.URL = pathutil.EnsureTrailingSlash(url)
		if strings.TrimSpace(repo.Name) == "" {
			repo.Name = repo.URL
		}
	}
	if contentBody, ok := extractNamedBlock(body, "content"); ok {
		repo = applyFilterBody(repo, contentBody)
	}
	return repo
}

func applyFilterBody(repo Repository, body string) Repository {
	repo.IncludeGroups = append(repo.IncludeGroups, parseRepeatedAssignments(body, `includeGroup\("([^"]+)"\)`)...)
	repo.IncludeGroupRegex = append(repo.IncludeGroupRegex, parseRepeatedAssignments(body, `includeGroupByRegex\("([^"]+)"\)`)...)
	repo.ExcludeGroups = append(repo.ExcludeGroups, parseRepeatedAssignments(body, `excludeGroup\("([^"]+)"\)`)...)
	repo.ExcludeGroupRegex = append(repo.ExcludeGroupRegex, parseRepeatedAssignments(body, `excludeGroupByRegex\("([^"]+)"\)`)...)
	for _, match := range regexp.MustCompile(`includeModule\("([^"]+)",\s*"([^"]+)"\)`).FindAllStringSubmatch(body, -1) {
		repo.IncludeModules = append(repo.IncludeModules, match[1]+":"+match[2])
	}
	for _, match := range regexp.MustCompile(`excludeModule\("([^"]+)",\s*"([^"]+)"\)`).FindAllStringSubmatch(body, -1) {
		repo.ExcludeModules = append(repo.ExcludeModules, match[1]+":"+match[2])
	}
	return repo
}

func dedupeRepositories(repos []Repository) []Repository {
	seen := map[string]int{}
	var out []Repository
	for _, repo := range repos {
		key := repo.Scope + "|" + repo.Name + "|" + repo.Kind + "|" + repo.URL
		if idx, ok := seen[key]; ok {
			existing := out[idx]
			existing.Exclusive = existing.Exclusive || repo.Exclusive
			if repo.Priority < existing.Priority {
				existing.Priority = repo.Priority
			}
			if existing.Origin == "" {
				existing.Origin = repo.Origin
			}
			existing.OfflineAllowed = existing.OfflineAllowed || repo.OfflineAllowed
			existing.IncludeGroups = mergeStrings(existing.IncludeGroups, repo.IncludeGroups)
			existing.IncludeGroupRegex = mergeStrings(existing.IncludeGroupRegex, repo.IncludeGroupRegex)
			existing.ExcludeGroups = mergeStrings(existing.ExcludeGroups, repo.ExcludeGroups)
			existing.ExcludeGroupRegex = mergeStrings(existing.ExcludeGroupRegex, repo.ExcludeGroupRegex)
			existing.IncludeModules = mergeStrings(existing.IncludeModules, repo.IncludeModules)
			existing.ExcludeModules = mergeStrings(existing.ExcludeModules, repo.ExcludeModules)
			out[idx] = existing
			continue
		}
		seen[key] = len(out)
		out = append(out, repo)
	}
	return out
}

func annotateRepositories(repos []Repository, origin string, startPriority int) []Repository {
	if len(repos) == 0 {
		return nil
	}
	out := make([]Repository, 0, len(repos))
	for i, repo := range repos {
		repo.Priority = startPriority + i
		if origin != "" {
			repo.Origin = origin
		}
		repo.OfflineAllowed = repo.OfflineAllowed || repositoryOfflineAllowed(repo)
		out = append(out, repo)
	}
	return out
}

func repositoryOfflineAllowed(repo Repository) bool {
	if repo.Kind == "mavenLocal" {
		return true
	}
	if strings.HasPrefix(repo.URL, "file:") || strings.HasPrefix(repo.URL, "file://") {
		return true
	}
	return false
}

func mergeStrings(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, values := range [][]string{a, b} {
		for _, value := range values {
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func loadVersionCatalogs(paths []string) (map[string]string, error) {
	out := map[string]string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		inVersions := false
		for _, raw := range strings.Split(string(data), "\n") {
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				inVersions = line == "[versions]"
				continue
			}
			if !inVersions {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			value := stripInlineComment(strings.TrimSpace(parts[1]))
			out[key] = value
		}
	}
	return out, nil
}

func loadVersionCatalogPluginAliases(paths []string) (map[string]string, error) {
	out := map[string]string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		inPlugins := false
		for _, raw := range strings.Split(string(data), "\n") {
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				inPlugins = line == "[plugins]"
				continue
			}
			if !inPlugins {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if !strings.HasPrefix(value, "{") {
				continue
			}
			id := parseCatalogPluginID(value)
			if id != "" {
				out[normalizePluginAlias(key)] = id
			}
		}
	}
	return out, nil
}

func parseCatalogPluginID(value string) string {
	value = strings.Trim(value, "{}")
	for _, field := range splitCatalogFields(value) {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) != "id" {
			continue
		}
		return stripInlineComment(strings.TrimSpace(parts[1]))
	}
	return ""
}

func splitCatalogFields(value string) []string {
	var fields []string
	var current strings.Builder
	inQuote := false
	for _, r := range value {
		switch r {
		case '"':
			inQuote = !inQuote
			current.WriteRune(r)
		case ',':
			if inQuote {
				current.WriteRune(r)
				continue
			}
			fields = append(fields, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if strings.TrimSpace(current.String()) != "" {
		fields = append(fields, strings.TrimSpace(current.String()))
	}
	return fields
}

func normalizePluginAlias(alias string) string {
	return strings.ReplaceAll(strings.TrimSpace(alias), "-", ".")
}

func stripInlineComment(v string) string {
	if idx := strings.Index(v, "#"); idx >= 0 {
		v = v[:idx]
	}
	return strings.TrimSpace(strings.Trim(v, `"`))
}

func resolveCatalogValue(prj *Project, value string) string {
	const prefix = "catalog:"
	if !strings.HasPrefix(value, prefix) {
		return strings.Trim(value, `"`)
	}
	key := strings.TrimPrefix(value, prefix)
	if resolved, ok := prj.VersionCatalogData[key]; ok {
		return strings.Trim(resolved, `"`)
	}
	return strings.Trim(value, `"`)
}
