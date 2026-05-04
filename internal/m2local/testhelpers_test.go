package m2local

import "github.com/kaeawc/grit/internal/project"

func newTestResolver(repos ...project.Repository) *Resolver {
	return New("/cache", "/work", repos, nil)
}
