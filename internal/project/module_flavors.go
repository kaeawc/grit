package project

import (
	"regexp"
	"strings"
)

func parseFlavorDimensions(body string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range parseQuotedListArgs(body, "flavorDimensions") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	re := regexp.MustCompile(`flavorDimensions\s*\+=\s*"([^"]+)"`)
	for _, match := range re.FindAllStringSubmatch(body, -1) {
		value := strings.TrimSpace(match[1])
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func parseProductFlavors(body string) map[string]ProductFlavor {
	block, ok := extractNamedBlock(body, "productFlavors")
	if !ok {
		return nil
	}
	re := regexp.MustCompile(`(?m)^\s*(?:create\("([^"]+)"\)|([A-Za-z0-9_]+))\s*\{`)
	indexes := re.FindAllStringSubmatchIndex(block, -1)
	out := map[string]ProductFlavor{}
	for _, idx := range indexes {
		if braceDepth(block[:idx[0]]) != 0 {
			continue
		}
		name := captureSubmatch(block, idx, 2)
		if name == "" {
			name = captureSubmatch(block, idx, 4)
		}
		if name == "" {
			continue
		}
		openIdx := idx[1] - 1
		flavorBody, _, ok := extractBraceBodyAt(block, openIdx)
		if !ok {
			continue
		}
		out[name] = ProductFlavor{
			Name:                name,
			Dimension:           parseAssignment(flavorBody, `dimension\s*=\s*"([^"]+)"`),
			ApplicationID:       parseAssignment(flavorBody, `applicationId\s*=\s*"([^"]+)"`),
			ApplicationIDSuffix: parseAssignment(flavorBody, `applicationIdSuffix\s*=\s*"([^"]+)"`),
			VersionCode:         parseAssignment(flavorBody, `versionCode\s*=\s*([0-9]+)`),
			VersionName:         parseAssignment(flavorBody, `versionName\s*=\s*"([^"]+)"`),
			VersionNameSuffix:   parseAssignment(flavorBody, `versionNameSuffix\s*=\s*"([^"]+)"`),
			MinSDK:              parseAssignment(flavorBody, `minSdk\s*=\s*(\d+)`),
			TargetSDK:           parseAssignment(flavorBody, `targetSdk\s*=\s*(\d+)`),
			MatchingFallbacks:   parseMatchingFallbacks(flavorBody),
			MissingDimensions:   parseMissingDimensionStrategies(flavorBody),
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
