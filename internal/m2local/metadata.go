package m2local

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

type moduleMetadata struct {
	Variants              []moduleVariant `json:"variants"`
	DependencyConstraints []moduleDep     `json:"dependencyConstraints"`
}

type moduleVariant struct {
	Name         string             `json:"name"`
	Attributes   map[string]string  `json:"attributes"`
	Capabilities []moduleCapability `json:"capabilities"`
	Dependencies []moduleDep        `json:"dependencies"`
	Files        []moduleFile       `json:"files"`
	AvailableAt  *moduleAvailableAt `json:"available-at"`
}

type moduleCapability struct {
	Group   string `json:"group"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type moduleAvailableAt struct {
	Group   string `json:"group"`
	Module  string `json:"module"`
	Version string `json:"version"`
}

type moduleDep struct {
	Group    string          `json:"group"`
	Module   string          `json:"module"`
	Excludes []moduleExclude `json:"excludes"`
	Version  struct {
		Requires string `json:"requires"`
	} `json:"version"`
}

type moduleExclude struct {
	Group  string `json:"group"`
	Module string `json:"module"`
}

type moduleFile struct {
	URL string `json:"url"`
}

func parseBOM(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, dep := range extractPOMDependencies(string(data), true) {
		out[dep.GroupID+":"+dep.ArtifactID] = dep.Version
	}
	return out, nil
}

func parsePOMDeps(path string) ([]Coordinate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	body := string(data)
	properties := extractPOMProperties(body)
	var out []Coordinate
	for _, dep := range extractPOMDependencies(body, false) {
		dep.Version = resolvePOMVersion(dep.Version, properties)
		if dep.Version == "" || dep.Scope == "test" || dep.Scope == "provided" || dep.Optional == "true" {
			continue
		}
		out = append(out, Coordinate{
			Group:    dep.GroupID,
			Module:   dep.ArtifactID,
			Version:  normalizeVersion(dep.Version),
			Excludes: dep.Exclusions,
		})
	}
	return out, nil
}

type pomDependency struct {
	GroupID    string
	ArtifactID string
	Version    string
	Scope      string
	Optional   string
	Exclusions []Exclude
}

func extractPOMProperties(body string) map[string]string {
	properties := map[string]string{}
	if projectVersion := extractProjectVersion(body); projectVersion != "" {
		properties["project.version"] = projectVersion
		properties["pom.version"] = projectVersion
	}
	propsSection := regexp.MustCompile(`(?s)<properties>(.*?)</properties>`).FindStringSubmatch(body)
	if len(propsSection) > 1 {
		entryRe := regexp.MustCompile(`(?s)<([A-Za-z0-9\.\-_]+)>(.*?)</([A-Za-z0-9\.\-_]+)>`)
		for _, match := range entryRe.FindAllStringSubmatch(propsSection[1], -1) {
			if len(match) < 4 || strings.TrimSpace(match[1]) != strings.TrimSpace(match[3]) {
				continue
			}
			properties[strings.TrimSpace(match[1])] = strings.TrimSpace(match[2])
		}
	}
	return properties
}

func extractProjectVersion(body string) string {
	body = regexp.MustCompile(`(?s)<parent>.*?</parent>`).ReplaceAllString(body, "")
	re := regexp.MustCompile(`(?s)<project\b.*?>.*?<groupId>.*?</groupId>.*?<artifactId>.*?</artifactId>.*?<version>(.*?)</version>`)
	match := re.FindStringSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func resolvePOMVersion(version string, properties map[string]string) string {
	version = strings.TrimSpace(version)
	if !strings.Contains(version, "${") {
		return version
	}
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	return strings.TrimSpace(re.ReplaceAllStringFunc(version, func(token string) string {
		nameMatch := re.FindStringSubmatch(token)
		if len(nameMatch) < 2 {
			return token
		}
		if value := properties[strings.TrimSpace(nameMatch[1])]; value != "" {
			return value
		}
		return token
	}))
}

func extractPOMDependencies(body string, dependencyManagement bool) []pomDependency {
	var section string
	if dependencyManagement {
		re := regexp.MustCompile(`(?s)<dependencyManagement>.*?<dependencies>(.*?)</dependencies>.*?</dependencyManagement>`)
		match := re.FindStringSubmatch(body)
		if len(match) < 2 {
			return nil
		}
		section = match[1]
	} else {
		re := regexp.MustCompile(`(?s)<dependencies>(.*?)</dependencies>`)
		matches := re.FindAllStringSubmatch(body, -1)
		if len(matches) == 0 {
			return nil
		}
		section = matches[len(matches)-1][1]
	}

	depRe := regexp.MustCompile(`(?s)<dependency>(.*?)</dependency>`)
	var out []pomDependency
	for _, depMatch := range depRe.FindAllStringSubmatch(section, -1) {
		block := depMatch[1]
		dep := pomDependency{
			GroupID:    extractTag(block, "groupId"),
			ArtifactID: extractTag(block, "artifactId"),
			Version:    extractTag(block, "version"),
			Scope:      extractTag(block, "scope"),
			Optional:   extractTag(block, "optional"),
			Exclusions: extractPOMExclusions(block),
		}
		if dep.GroupID != "" && dep.ArtifactID != "" && dep.Version != "" {
			out = append(out, dep)
		}
	}
	return out
}

func extractPOMExclusions(body string) []Exclude {
	re := regexp.MustCompile(`(?s)<exclusion>(.*?)</exclusion>`)
	matches := re.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]Exclude, 0, len(matches))
	for _, match := range matches {
		block := match[1]
		group := extractTag(block, "groupId")
		module := extractTag(block, "artifactId")
		if group == "" && module == "" {
			continue
		}
		out = append(out, Exclude{Group: group, Module: module})
	}
	return out
}

func extractTag(body, tag string) string {
	re := regexp.MustCompile(fmt.Sprintf(`(?s)<%s>(.*?)</%s>`, tag, tag))
	match := re.FindStringSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func chooseVariant(variants []moduleVariant) *moduleVariant {
	score := func(v *moduleVariant) int {
		if isDocumentationVariant(v) {
			return -1
		}
		score := 0
		switch v.Attributes["org.jetbrains.kotlin.platform.type"] {
		case "androidJvm":
			score += 100
		case "jvm":
			score += 90
		case "common":
			score -= 20
		}
		switch v.Attributes["org.gradle.usage"] {
		case "java-runtime":
			score += 20
		case "java-api":
			score += 10
		case "kotlin-api":
			score += 8
		case "kotlin-runtime":
			score += 6
		case "kotlin-metadata":
			score -= 10
		}
		if strings.Contains(strings.ToLower(v.Name), "release") {
			score += 5
		}
		if hasBinaryArtifact(v) {
			score += 2
		}
		return score
	}
	var best *moduleVariant
	bestScore := -1
	for i := range variants {
		v := &variants[i]
		s := score(v)
		if s > bestScore {
			best = v
			bestScore = s
		}
	}
	if bestScore >= 0 {
		return best
	}
	for i := range variants {
		if hasBinaryArtifact(&variants[i]) {
			return &variants[i]
		}
	}
	return nil
}

func hasBinaryArtifact(v *moduleVariant) bool {
	for _, file := range v.Files {
		if isBinaryArtifactName(file.URL) {
			return true
		}
	}
	return false
}

func isDocumentationVariant(v *moduleVariant) bool {
	if docType := strings.ToLower(v.Attributes["org.gradle.docstype"]); docType != "" {
		return true
	}
	if category := strings.ToLower(v.Attributes["org.gradle.category"]); category == "documentation" {
		return true
	}
	name := strings.ToLower(v.Name)
	return strings.Contains(name, "sources") || strings.Contains(name, "javadoc")
}

func coordinateID(coord Coordinate) string {
	id := "maven:" + coord.Group + ":" + coord.Module + ":" + coord.Version
	if len(coord.Excludes) == 0 {
		return id
	}
	var parts []string
	for _, exclude := range coord.Excludes {
		parts = append(parts, exclude.Group+":"+exclude.Module)
	}
	return id + "|exclude=" + strings.Join(parts, ",")
}

func coordinateFromID(id string) (Coordinate, bool) {
	parts := strings.Split(id, ":")
	if len(parts) != 4 || parts[0] != "maven" {
		return Coordinate{}, false
	}
	return Coordinate{
		Group:   parts[1],
		Module:  parts[2],
		Version: parts[3],
	}, true
}

func uniqueAndroidLibraries(libs []AndroidLibrary) []AndroidLibrary {
	seen := map[string]bool{}
	var out []AndroidLibrary
	for _, lib := range libs {
		if lib.ID == "" || seen[lib.ID] {
			continue
		}
		seen[lib.ID] = true
		out = append(out, lib)
	}
	return out
}

func isBinaryArtifactName(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, "-sources.jar") || strings.HasSuffix(lower, "-javadoc.jar") {
		return false
	}
	return strings.HasSuffix(lower, ".jar") || strings.HasSuffix(lower, ".aar")
}

func toCoordinates(deps []moduleDep) []Coordinate {
	return toCoordinatesWithConstraints(deps, nil)
}

func toCoordinatesWithConstraints(deps []moduleDep, constraints map[string]string) []Coordinate {
	var out []Coordinate
	for _, dep := range deps {
		version := normalizeVersion(dep.Version.Requires)
		if version == "" && constraints != nil {
			version = constraints[dep.Group+":"+dep.Module]
		}
		if version == "" {
			continue
		}
		out = append(out, Coordinate{Group: dep.Group, Module: dep.Module, Version: version, Excludes: toExcludes(dep.Excludes)})
	}
	return out
}

func constraintVersions(deps []moduleDep) map[string]string {
	out := map[string]string{}
	for _, dep := range deps {
		version := normalizeVersion(dep.Version.Requires)
		if version == "" {
			continue
		}
		out[dep.Group+":"+dep.Module] = version
	}
	return out
}

func toExcludes(excludes []moduleExclude) []Exclude {
	if len(excludes) == 0 {
		return nil
	}
	out := make([]Exclude, 0, len(excludes))
	for _, exclude := range excludes {
		out = append(out, Exclude{Group: exclude.Group, Module: exclude.Module})
	}
	return out
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "[")
	v = strings.TrimPrefix(v, "(")
	v = strings.TrimSuffix(v, "]")
	v = strings.TrimSuffix(v, ")")
	if idx := strings.Index(v, ","); idx >= 0 {
		v = v[:idx]
	}
	return strings.TrimSpace(v)
}

func lookupManagedVersion(platforms map[string]map[string]string, group, module string) string {
	key := group + ":" + module
	for _, managed := range platforms {
		if version, ok := managed[key]; ok {
			return version
		}
	}
	return ""
}

func clonePlatforms(in map[string]map[string]string) map[string]map[string]string {
	out := map[string]map[string]string{}
	for k, v := range in {
		inner := map[string]string{}
		for kk, vv := range v {
			inner[kk] = vv
		}
		out[k] = inner
	}
	return out
}

func mergeUnique(parts ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, part := range parts {
		for _, item := range part {
			if !seen[item] {
				seen[item] = true
				out = append(out, item)
			}
		}
	}
	return out
}

func (r *Resolver) resolveModuleMetadata(coord Coordinate, path string, source *ResolutionMetadataSource) (string, *AndroidLibrary, []Coordinate, error) {
	var mod moduleMetadata
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, nil, err
	}
	if err := json.Unmarshal(data, &mod); err != nil {
		return "", nil, nil, err
	}

	chosen := chooseVariant(mod.Variants)
	if chosen == nil {
		return "", nil, nil, fmt.Errorf("no suitable variant for %s:%s:%s", coord.Group, coord.Module, coord.Version)
	}
	if len(mod.Variants) > 1 {
		alternates := make([]string, 0, len(mod.Variants))
		for _, variant := range mod.Variants {
			if variant.Name == chosen.Name {
				continue
			}
			alternates = append(alternates, variant.Name)
		}
		r.addSelection(ResolutionSelection{
			Kind:           "variant_selection",
			Coordinate:     coord.Group + ":" + coord.Module + ":" + coord.Version,
			Chosen:         chosen.Name,
			Reason:         "best_scored_variant",
			Alternates:     alternates,
			Attributes:     cloneStringMap(chosen.Attributes),
			Capabilities:   capabilityIDs(chosen.Capabilities),
			MetadataSource: cloneMetadataSource(source),
		})
	}
	if capabilities := capabilityIDs(chosen.Capabilities); len(capabilities) > 0 {
		r.addSelection(ResolutionSelection{
			Kind:           "capability_selection",
			Coordinate:     coord.Group + ":" + coord.Module + ":" + coord.Version,
			Chosen:         chosen.Name,
			Reason:         "declared_variant_capabilities",
			Attributes:     cloneStringMap(chosen.Attributes),
			Capabilities:   capabilities,
			MetadataSource: cloneMetadataSource(source),
		})
	}

	if chosen.AvailableAt != nil {
		r.addSelection(ResolutionSelection{
			Kind:           "available_at_redirect",
			Coordinate:     coord.Group + ":" + coord.Module + ":" + coord.Version,
			Chosen:         chosen.AvailableAt.Group + ":" + chosen.AvailableAt.Module + ":" + chosen.AvailableAt.Version,
			Reason:         chosen.Name,
			MetadataSource: cloneMetadataSource(source),
		})
		return r.resolveOne(Coordinate{
			Group:   chosen.AvailableAt.Group,
			Module:  chosen.AvailableAt.Module,
			Version: chosen.AvailableAt.Version,
		})
	}

	deps := toCoordinatesWithConstraints(chosen.Dependencies, constraintVersions(mod.DependencyConstraints))
	artifact, androidLibrary, err := r.resolveVariantArtifact(coord, chosen)
	if err != nil {
		return "", nil, nil, err
	}
	binding := ""
	switch {
	case androidLibrary != nil:
		binding = "android-library"
	case artifact != "":
		binding = "jar"
	}
	if binding != "" {
		r.addSelection(ResolutionSelection{
			Kind:           "realization_binding",
			Coordinate:     coord.Group + ":" + coord.Module + ":" + coord.Version,
			Chosen:         chosen.Name,
			Reason:         "resolved_artifact",
			Binding:        binding,
			Attributes:     cloneStringMap(chosen.Attributes),
			Capabilities:   capabilityIDs(chosen.Capabilities),
			MetadataSource: cloneMetadataSource(source),
		})
	}
	if artifact != "" || androidLibrary != nil {
		r.addReplayPin(ResolutionPin{
			Coordinate:    coord.Group + ":" + coord.Module + ":" + coord.Version,
			Variant:       chosen.Name,
			Binding:       binding,
			Capabilities:  capabilityIDs(chosen.Capabilities),
			RepositoryURL: repositoryURLForMetadataSource(source),
		})
	}
	return artifact, androidLibrary, deps, nil
}

func repositoryURLForMetadataSource(source *ResolutionMetadataSource) string {
	if source == nil {
		return ""
	}
	return source.RepositoryURL
}

func cloneMetadataSource(source *ResolutionMetadataSource) *ResolutionMetadataSource {
	if source == nil {
		return nil
	}
	copy := *source
	return &copy
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func capabilityIDs(capabilities []moduleCapability) []string {
	if len(capabilities) == 0 {
		return nil
	}
	out := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		if capability.Group == "" || capability.Name == "" || capability.Version == "" {
			continue
		}
		out = append(out, capability.Group+":"+capability.Name+":"+capability.Version)
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return out
}

func (r *Resolver) resolveVariantArtifact(coord Coordinate, variant *moduleVariant) (string, *AndroidLibrary, error) {
	for _, file := range variant.Files {
		if !isBinaryArtifactName(file.URL) {
			continue
		}
		base := r.moduleBasePath(coord)
		found, err := findNamedFile(base, file.URL)
		if err != nil {
			return "", nil, err
		}
		return r.normalizeArtifact(found)
	}
	return "", nil, nil
}
