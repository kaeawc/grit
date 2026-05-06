package project

import "regexp"

func parseMatchingFallbacks(body string) []string {
	var out []string
	reList := regexp.MustCompile(`matchingFallbacks\s*(?:\+=|=)\s*listOf\((?s)(.*?)\)`)
	for _, match := range reList.FindAllStringSubmatch(body, -1) {
		out = appendUniqueQuoted(out, match[1])
	}
	reArray := regexp.MustCompile(`matchingFallbacks\s*=\s*\[(?s)(.*?)\]`)
	for _, match := range reArray.FindAllStringSubmatch(body, -1) {
		out = appendUniqueQuoted(out, match[1])
	}
	reSingle := regexp.MustCompile(`matchingFallbacks\s*\+=\s*"([^"]+)"`)
	for _, match := range reSingle.FindAllStringSubmatch(body, -1) {
		if len(match) > 1 && !containsString(out, match[1]) {
			out = append(out, match[1])
		}
	}
	return out
}

func parseMissingDimensionStrategies(body string) map[string][]string {
	re := regexp.MustCompile(`missingDimensionStrategy\s*\(\s*"([^"]+)"\s*,(?s)(.*?)\)`)
	matches := re.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	out := map[string][]string{}
	for _, match := range matches {
		values := parseQuotedArguments(match[2])
		if len(values) == 0 {
			continue
		}
		out[match[1]] = values
	}
	return out
}

func parseQuotedArguments(body string) []string {
	return parseQuotedList(body)
}

func appendUniqueQuoted(dst []string, body string) []string {
	for _, value := range parseQuotedList(body) {
		if !containsString(dst, value) {
			dst = append(dst, value)
		}
	}
	return dst
}

func parseQuotedList(body string) []string {
	re := regexp.MustCompile(`"([^"]+)"`)
	matches := re.FindAllStringSubmatch(body, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match[1])
	}
	return out
}
