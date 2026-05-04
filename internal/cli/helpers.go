package cli

import (
	"regexp"
	"strings"
)

func stripBoolFlag(args []string, name string) ([]string, bool) {
	out := make([]string, 0, len(args))
	found := false
	for _, arg := range args {
		if arg == name {
			found = true
			continue
		}
		out = append(out, arg)
	}
	return out, found
}

func hasOption(args []string, name string) bool {
	for i, arg := range args {
		if arg == name {
			return true
		}
		if strings.HasPrefix(arg, name+"=") {
			return true
		}
		if i > 0 && args[i-1] == name {
			return true
		}
	}
	return false
}

func parseAPKPath(stdout string) string {
	re := regexp.MustCompile(`(?m)(?:assembled|installed)\s+\S+\s+APK:\s+(.+)$`)
	matches := re.FindStringSubmatch(stdout)
	if len(matches) == 2 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

func taskNameForVariant(prefix, variantName string) string {
	if variantName == "" {
		return prefix
	}
	if len(variantName) == 1 {
		return prefix + strings.ToUpper(variantName)
	}
	return prefix + strings.ToUpper(variantName[:1]) + variantName[1:]
}
