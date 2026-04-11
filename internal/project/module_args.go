package project

import "regexp"

func parseFreeCompilerArgs(body string) []string {
	block, ok := extractNamedBlock(body, "compilerOptions")
	if !ok {
		return nil
	}
	re := regexp.MustCompile(`freeCompilerArgs\s*=\s*listOf\((?s)(.*?)\)`)
	match := re.FindStringSubmatch(block)
	if len(match) < 2 {
		return nil
	}
	quoted := regexp.MustCompile(`"([^"]+)"`)
	var out []string
	for _, part := range quoted.FindAllStringSubmatch(match[1], -1) {
		out = append(out, part[1])
	}
	return out
}

func parseLintDisabledChecks(body string) []string {
	block, ok := extractNamedBlock(body, "lint")
	if !ok {
		return nil
	}
	re := regexp.MustCompile(`disable\s*\+=\s*"([^"]+)"`)
	var out []string
	for _, match := range re.FindAllStringSubmatch(block, -1) {
		out = append(out, match[1])
	}
	return out
}

func parseQuotedListArgs(body, fn string) []string {
	re := regexp.MustCompile(fn + `\((?s)(.*?)\)`)
	match := re.FindStringSubmatch(body)
	if len(match) < 2 {
		return nil
	}
	quoted := regexp.MustCompile(`"([^"]+)"`)
	var out []string
	for _, q := range quoted.FindAllStringSubmatch(match[1], -1) {
		out = append(out, q[1])
	}
	return out
}
