package project

import "path/filepath"

func resolveRelativeFiles(base string, rels []string) []string {
	if len(rels) == 0 {
		return nil
	}
	out := make([]string, 0, len(rels))
	for _, rel := range rels {
		if filepath.IsAbs(rel) {
			out = append(out, rel)
			continue
		}
		out = append(out, filepath.Join(base, rel))
	}
	return out
}
