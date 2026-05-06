package project

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func parseBuildTypes(body string, modDir string) map[string]BuildType {
	block, ok := extractNamedBlock(body, "buildTypes")
	if !ok {
		return nil
	}
	out := map[string]BuildType{}
	re := regexp.MustCompile(`(?m)^\s*(?:([A-Za-z0-9_]+)|(?:create|getByName|named|register)\("([^"]+)"\))\s*\{`)
	indexes := re.FindAllStringSubmatchIndex(block, -1)
	for _, idx := range indexes {
		if braceDepth(block[:idx[0]]) != 0 {
			continue
		}
		name := firstNonEmpty(
			captureSubmatch(block, idx, 2),
			captureSubmatch(block, idx, 4),
		)
		if name == "" {
			continue
		}
		openIdx := idx[1] - 1
		buildBody, _, ok := extractBraceBodyAt(block, openIdx)
		if !ok {
			continue
		}
		out[name] = parseBuildTypeBody(name, buildBody, modDir)
	}
	return out
}

func parseBuildTypeBody(name, body, modDir string) BuildType {
	buildType := BuildType{
		Name:                name,
		IsMinifyEnabled:     strings.Contains(body, "isMinifyEnabled = true"),
		IsShrinkResources:   strings.Contains(body, "isShrinkResources = true"),
		SigningConfig:       parseAssignment(body, `signingConfig\s*=\s*signingConfigs\.getByName\("([^"]+)"\)`),
		ApplicationID:       parseAssignment(body, `applicationId\s*=\s*"([^"]+)"`),
		ApplicationIDSuffix: parseAssignment(body, `applicationIdSuffix\s*=\s*"([^"]+)"`),
		VersionCode:         parseAssignment(body, `versionCode\s*=\s*([0-9]+)`),
		VersionName:         parseAssignment(body, `versionName\s*=\s*"([^"]+)"`),
		VersionNameSuffix:   parseAssignment(body, `versionNameSuffix\s*=\s*"([^"]+)"`),
		MinSDK:              parseAssignment(body, `minSdk\s*=\s*(\d+)`),
		TargetSDK:           parseAssignment(body, `targetSdk\s*=\s*(\d+)`),
		MatchingFallbacks:   parseMatchingFallbacks(body),
	}
	if initWith := parseAssignment(body, `initWith\s*\(\s*getByName\("([^"]+)"\)\s*\)`); initWith != "" && !containsString(buildType.MatchingFallbacks, initWith) {
		buildType.MatchingFallbacks = append(buildType.MatchingFallbacks, initWith)
	}
	if buildType.SigningConfig == "" {
		buildType.SigningConfig = parseAssignment(body, `signingConfig\s*=\s*signingConfigs\.named\("([^"]+)"\)`)
	}
	if buildType.SigningConfig == "" {
		buildType.SigningConfig = parseAssignment(body, `signingConfig\s*=\s*signingConfigs\["([^"]+)"\]`)
	}
	if buildType.SigningConfig == "" {
		if release := parseAssignment(body, `if\s*\([^)]+\)\s*\{\s*signingConfigs\.getByName\("([^"]+)"\)`); release != "" {
			fallback := parseAssignment(body, `else\s*\{\s*signingConfigs\.getByName\("([^"]+)"\)`)
			if fallback != "" {
				buildType.SigningConfig = fallback + "|" + release
			} else {
				buildType.SigningConfig = release
			}
		}
	}
	if buildType.SigningConfig == "" && strings.Contains(body, `signingConfigs.getByName("debug")`) {
		buildType.SigningConfig = "debug"
	}
	buildType.Optimization = VariantOptimization{
		MinifyEnabled:        buildType.IsMinifyEnabled,
		ShrinkResources:      buildType.IsShrinkResources,
		PackageOptimizations: parsePackageOptimizations(body),
	}
	defaultRule := parseAssignment(body, `getDefaultProguardFile\("([^"]+)"\)`)
	for _, rel := range parseQuotedListArgs(body, "proguardFiles") {
		if rel == defaultRule || strings.HasPrefix(rel, "proguard-android") {
			continue
		}
		buildType.ProguardFiles = append(buildType.ProguardFiles, filepath.Join(modDir, rel))
	}
	if defaultRule != "" {
		buildType.ProguardFiles = append([]string{filepath.Join(os.Getenv("HOME"), "Library", "Android", "sdk", "tools", "proguard", defaultRule)}, buildType.ProguardFiles...)
	}
	return buildType
}

func parsePackageOptimizations(body string) []PackageOptimization {
	block, ok := extractNamedBlock(body, "packageOptimizations")
	if !ok {
		return nil
	}
	re := regexp.MustCompile(`(?ms)(?:package|packageName)\("([^"]+)"\)\s*(\{)?`)
	indexes := re.FindAllStringSubmatchIndex(block, -1)
	if len(indexes) == 0 {
		return nil
	}
	var out []PackageOptimization
	for _, idx := range indexes {
		name := captureSubmatch(block, idx, 2)
		if name == "" {
			continue
		}
		openIdx := idx[1] - 1
		entry := PackageOptimization{PackageName: name}
		if openIdx >= 0 && openIdx < len(block) && block[openIdx] == '{' {
			entryBody, _, ok := extractBraceBodyAt(block, openIdx)
			if ok {
				if v, ok := parseOptionalBool(entryBody, `(?:minifyEnabled|isMinifyEnabled)\s*=\s*(true|false)`); ok {
					entry.MinifyEnabled = v
				}
				if v, ok := parseOptionalBool(entryBody, `(?:shrinkResources|isShrinkResources)\s*=\s*(true|false)`); ok {
					entry.ShrinkResources = v
				}
				if note := parseAssignment(entryBody, `note\s*=\s*"([^"]+)"`); note != "" {
					entry.Note = note
				}
			}
		}
		out = append(out, entry)
	}
	return out
}
