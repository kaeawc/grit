package dependencywiring

import (
	"reflect"
	"testing"

	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

type fakeArtifactResolver struct {
	deps     *modulebuild.Dependencies
	resolved *m2local.Resolved
	err      error
}

func (r *fakeArtifactResolver) Resolve(deps *modulebuild.Dependencies) (*m2local.Resolved, error) {
	r.deps = deps
	return r.resolved, r.err
}

func TestResolveMetroCompilerPluginUsesResolverPath(t *testing.T) {
	resolver := &fakeArtifactResolver{
		resolved: &m2local.Resolved{
			RuntimeJars: []string{"/work/.grit/worktree/materialized-m2/dev/zacsweers/metro/compiler/0.13.0/compiler-0.13.0.jar"},
		},
	}
	prj := &project.Project{
		VersionCatalogData: map[string]string{"metro": "0.13.0"},
	}

	got := ResolveMetroCompilerPlugin(prj, resolver)
	want := "/work/.grit/worktree/materialized-m2/dev/zacsweers/metro/compiler/0.13.0/compiler-0.13.0.jar"
	if got != want {
		t.Fatalf("ResolveMetroCompilerPlugin = %q want %q", got, want)
	}

	wantDeps := &modulebuild.Dependencies{
		Main: []modulebuild.Ref{{Kind: "raw", Value: "dev.zacsweers.metro:compiler:0.13.0"}},
	}
	if !reflect.DeepEqual(resolver.deps, wantDeps) {
		t.Fatalf("resolved deps = %#v want %#v", resolver.deps, wantDeps)
	}
}

func TestResolveMetroCompilerPluginIgnoresNonMetroCompilerJar(t *testing.T) {
	resolver := &fakeArtifactResolver{
		resolved: &m2local.Resolved{
			RuntimeJars: []string{"/cache/com/example/compiler/0.13.0/compiler-0.13.0.jar"},
		},
	}
	prj := &project.Project{
		VersionCatalogData: map[string]string{"metro": "0.13.0"},
	}

	if got := ResolveMetroCompilerPlugin(prj, resolver); got != "" {
		t.Fatalf("expected non-Metro compiler jar to be ignored, got %q", got)
	}
}
