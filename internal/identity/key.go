package identity

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

func Key(namespace string, parts ...string) string {
	return buildKey(namespace, parts...)
}

func SetKey(namespace string, parts ...string) string {
	return buildKey(namespace, NormalizeSetParts(parts...)...)
}

func NormalizePathPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return path.Clean(strings.ReplaceAll(value, "\\", "/"))
}

func NormalizePathParts(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := NormalizePathPart(value)
		if normalized == "" {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

func NormalizeSetParts(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := NormalizeLogicalPart(value)
		if normalized == "" {
			continue
		}
		out = append(out, normalized)
	}
	sort.Strings(out)
	out = dedupeSorted(out)
	return out
}

func buildKey(namespace string, parts ...string) string {
	var builder strings.Builder
	builder.Grow(len(namespace) + len(parts)*8)
	builder.WriteString(strings.TrimSpace(namespace))
	for _, part := range parts {
		builder.WriteByte('|')
		builder.WriteString(encodeKeyPart(part))
	}
	return builder.String()
}

func encodeKeyPart(part string) string {
	part = strings.TrimSpace(strings.ReplaceAll(part, "\n", " "))
	return fmt.Sprintf("%d:%s", len(part), part)
}

func dedupeSorted(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}
