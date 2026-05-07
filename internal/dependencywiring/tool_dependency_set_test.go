package dependencywiring

import (
	"errors"
	"reflect"
	"testing"

	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
)

func TestResolveToolDependencySetResolvesAllCoordinatesOnce(t *testing.T) {
	resolver := &toolSetResolver{
		result: &m2local.Resolved{
			CompileJars: []string{
				"/work/.grit/worktree/materialized-m2/org/jetbrains/kotlin/kotlin-compiler-embeddable/2.3.3/kotlin-compiler-embeddable-2.3.3.jar",
				"/work/.grit/worktree/materialized-m2/org/jetbrains/kotlin/kotlin-stdlib/2.3.3/kotlin-stdlib-2.3.3.jar",
			},
			RuntimeJars: []string{
				"/work/.grit/worktree/materialized-m2/org/jetbrains/kotlin/kotlin-stdlib/2.3.3/kotlin-stdlib-2.3.3.jar",
				"/work/.grit/worktree/materialized-m2/org/jetbrains/kotlin/kotlin-reflect/2.3.3/kotlin-reflect-2.3.3.jar",
			},
		},
	}
	got, err := ResolveToolDependencySet(resolver, ToolDependencySet{
		Name: "kotlin",
		Dependencies: []ToolDependency{
			{Group: "org.jetbrains.kotlin", Module: "kotlin-compiler-embeddable", Version: "2.3.3", Role: "compiler"},
			{Group: "org.jetbrains.kotlin", Module: "kotlin-stdlib", Version: "2.3.3", Role: "runtime"},
			{Group: "org.jetbrains.kotlin", Module: "kotlin-reflect", Version: "2.3.3", Role: "reflect"},
		},
	})
	if err != nil {
		t.Fatalf("ResolveToolDependencySet returned error: %v", err)
	}
	wantRefs := []modulebuild.Ref{
		{Kind: "raw", Value: "org.jetbrains.kotlin:kotlin-compiler-embeddable:2.3.3"},
		{Kind: "raw", Value: "org.jetbrains.kotlin:kotlin-stdlib:2.3.3"},
		{Kind: "raw", Value: "org.jetbrains.kotlin:kotlin-reflect:2.3.3"},
	}
	if !reflect.DeepEqual(resolver.deps.Main, wantRefs) {
		t.Fatalf("resolved refs = %#v want %#v", resolver.deps.Main, wantRefs)
	}
	if got.FirstJar("compiler") == "" {
		t.Fatalf("expected compiler role jar, got %#v", got.ByRole)
	}
	if got.FirstJar("reflect") == "" {
		t.Fatalf("expected reflect role jar, got %#v", got.ByRole)
	}
	if jars := got.Jars("runtime"); len(jars) != 1 {
		t.Fatalf("expected de-duplicated runtime role jar, got %#v", jars)
	}
}

func TestResolveToolDependencySetClassifiesLegacyGradleCachePaths(t *testing.T) {
	resolver := &toolSetResolver{
		result: &m2local.Resolved{
			RuntimeJars: []string{"/gradle/org.jetbrains.kotlin/kotlin-serialization-compiler-plugin-embeddable/2.3.3/hash/kotlin-serialization-compiler-plugin-embeddable-2.3.3.jar"},
		},
	}
	got, err := ResolveToolDependencySet(resolver, ToolDependencySet{
		Dependencies: []ToolDependency{
			{Group: "org.jetbrains.kotlin", Module: "kotlin-serialization-compiler-plugin-embeddable", Version: "2.3.3", Role: "serialization-plugin"},
		},
	})
	if err != nil {
		t.Fatalf("ResolveToolDependencySet returned error: %v", err)
	}
	if got.FirstJar("serialization-plugin") == "" {
		t.Fatalf("expected fallback path classification, got %#v", got.ByRole)
	}
}

func TestResolveToolDependencySetIgnoresMissingOptionalArtifacts(t *testing.T) {
	resolver := &optionalToolSetResolver{
		required: &m2local.Resolved{
			RuntimeJars: []string{"/m2/org/jetbrains/kotlin/kotlin-stdlib/2.3.3/kotlin-stdlib-2.3.3.jar"},
		},
		optionalErr: errors.New("not found"),
	}
	got, err := ResolveToolDependencySet(resolver, ToolDependencySet{
		Dependencies: []ToolDependency{
			{Group: "org.jetbrains.kotlin", Module: "kotlin-stdlib", Version: "2.3.3", Role: "runtime"},
			{Group: "org.jetbrains.kotlin", Module: "kotlin-missing-plugin", Version: "2.3.3", Role: "missing-plugin", Optional: true},
		},
	})
	if err != nil {
		t.Fatalf("ResolveToolDependencySet returned error: %v", err)
	}
	if len(resolver.calls) != 2 {
		t.Fatalf("expected required and optional resolve calls, got %#v", resolver.calls)
	}
	if got.FirstJar("runtime") == "" {
		t.Fatalf("expected required dependency to remain resolved, got %#v", got.ByRole)
	}
	if got.FirstJar("missing-plugin") != "" {
		t.Fatalf("expected missing optional role to be empty, got %#v", got.ByRole)
	}
}

type toolSetResolver struct {
	deps   modulebuild.Dependencies
	result *m2local.Resolved
}

func (r *toolSetResolver) Resolve(deps *modulebuild.Dependencies) (*m2local.Resolved, error) {
	if deps != nil {
		r.deps = *deps
	}
	return r.result, nil
}

type optionalToolSetResolver struct {
	calls       []modulebuild.Dependencies
	required    *m2local.Resolved
	optionalErr error
}

func (r *optionalToolSetResolver) Resolve(deps *modulebuild.Dependencies) (*m2local.Resolved, error) {
	if deps != nil {
		r.calls = append(r.calls, *deps)
	}
	if len(r.calls) == 1 {
		return r.required, nil
	}
	return nil, r.optionalErr
}
