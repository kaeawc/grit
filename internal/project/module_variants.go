package project

import (
	"fmt"
	"regexp"
	"strings"
)

func parseEnabledVariants(body string) []string {
	if !strings.Contains(body, "beforeVariants") || !strings.Contains(body, "variant.enable") {
		return nil
	}
	var out []string
	for _, match := range regexp.MustCompile(`variant\.enable\s*=\s*variant\.name\s+in\s+([A-Za-z_][A-Za-z0-9_]*)`).FindAllStringSubmatch(body, -1) {
		if len(match) < 2 {
			continue
		}
		out = append(out, parseStringListVariable(body, match[1])...)
	}
	for _, match := range regexp.MustCompile(`(?s)variant\.enable\s*=\s*variant\.name\s+in\s+listOf\s*\((.*?)\)`).FindAllStringSubmatch(body, -1) {
		if len(match) < 2 {
			continue
		}
		out = append(out, parseQuotedStrings(match[1])...)
	}
	return uniqueNonEmptyStrings(out)
}

func parseStringListVariable(body, name string) []string {
	pattern := fmt.Sprintf(`(?s)\b(?:val|var)\s+%s\s*=\s*listOf\s*\((.*?)\)`, regexp.QuoteMeta(strings.TrimSpace(name)))
	match := regexp.MustCompile(pattern).FindStringSubmatch(body)
	if len(match) < 2 {
		return nil
	}
	return parseQuotedStrings(match[1])
}

func parseQuotedStrings(body string) []string {
	matches := regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(body, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			out = append(out, match[1])
		}
	}
	return out
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
