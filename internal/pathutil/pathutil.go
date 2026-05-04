package pathutil

import "strings"

func EnsureTrailingSlash(v string) string {
	if v == "" || strings.HasSuffix(v, "/") {
		return v
	}
	return v + "/"
}
