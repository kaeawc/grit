package project

import (
	"regexp"
	"strings"
)

func parseCustomVariants(body string, modDir string) map[string]BuildType {
	gritBlock, ok := extractNamedBlock(body, "grit")
	if !ok {
		return nil
	}
	variantsBlock, ok := extractNamedBlock(gritBlock, "variants")
	if !ok {
		return nil
	}
	re := regexp.MustCompile(`(?m)^\s*(?:create\("([^"]+)"\)|register\("([^"]+)"\)|([A-Za-z0-9_]+))\s*\{`)
	indexes := re.FindAllStringSubmatchIndex(variantsBlock, -1)
	if len(indexes) == 0 {
		return nil
	}
	out := map[string]BuildType{}
	for _, idx := range indexes {
		if braceDepth(variantsBlock[:idx[0]]) != 0 {
			continue
		}
		name := firstNonEmpty(
			captureSubmatch(variantsBlock, idx, 2),
			captureSubmatch(variantsBlock, idx, 4),
			captureSubmatch(variantsBlock, idx, 6),
		)
		if name == "" {
			continue
		}
		variantBody, _, ok := extractBraceBodyAt(variantsBlock, idx[1]-1)
		if !ok {
			continue
		}
		custom := parseBuildTypeBody(name, variantBody, modDir)
		custom.DeclaredName = firstNonEmpty(
			parseAssignment(variantBody, `declaredName\s*=\s*"([^"]+)"`),
			name,
		)
		custom.BaseBuildType = firstNonEmpty(
			parseAssignment(variantBody, `baseBuildType\s*=\s*"([^"]+)"`),
			parseAssignment(variantBody, `fromBuildType\s*\(\s*"([^"]+)"\s*\)`),
		)
		custom.Flavors = parseVariantFlavors(variantBody)
		out[name] = custom
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseVariantFlavors(body string) []string {
	var out []string
	for _, match := range []*regexp.Regexp{
		regexp.MustCompile(`flavors\s*=\s*listOf\((?s)(.*?)\)`),
		regexp.MustCompile(`flavors\s*=\s*\[(?s)(.*?)\]`),
		regexp.MustCompile(`flavors\s*\((?s)(.*?)\)`),
		regexp.MustCompile(`flavors\s*\+=\s*listOf\((?s)(.*?)\)`),
	} {
		for _, value := range match.FindAllStringSubmatch(body, -1) {
			if len(value) < 2 {
				continue
			}
			out = appendUniqueQuoted(out, value[1])
		}
	}
	reSingle := regexp.MustCompile(`flavors\s*\+=\s*"([^"]+)"`)
	for _, value := range reSingle.FindAllStringSubmatch(body, -1) {
		if len(value) < 2 {
			continue
		}
		flavor := strings.TrimSpace(value[1])
		if flavor == "" || containsString(out, flavor) {
			continue
		}
		out = append(out, flavor)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeBuildTypeMaps(base map[string]BuildType, overlays ...map[string]BuildType) map[string]BuildType {
	size := len(base)
	for _, overlay := range overlays {
		size += len(overlay)
	}
	if size == 0 {
		return nil
	}
	out := make(map[string]BuildType, size)
	for name, buildType := range base {
		out[name] = buildType
	}
	for _, overlay := range overlays {
		for name, buildType := range overlay {
			out[name] = buildType
		}
	}
	return out
}
