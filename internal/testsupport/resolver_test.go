package testsupport

import (
	"errors"
	"testing"

	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
)

func TestWiringResolverRecorderReturnsConfiguredResult(t *testing.T) {
	resolver := NewWiringResolverRecorder()
	resolver.Result = &m2local.Resolved{
		CompileJars: []string{"lib.jar"},
	}
	deps := &modulebuild.Dependencies{
		Main: []modulebuild.Ref{{Kind: "external", Value: "org.example:lib:1.0"}},
	}

	got, err := resolver.Resolve(deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.CompileJars) != 1 || got.CompileJars[0] != "lib.jar" {
		t.Fatalf("got = %#v", got)
	}

	calls := resolver.CallsSnapshot()
	if len(calls) != 1 || len(calls[0].Main) != 1 || calls[0].Main[0].Value != "org.example:lib:1.0" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestWiringResolverRecorderReturnsError(t *testing.T) {
	resolver := NewWiringResolverRecorder()
	sentinel := errors.New("resolution failed")
	resolver.Err = sentinel

	_, err := resolver.Resolve(&modulebuild.Dependencies{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestWiringResolverRecorderCopiesResolveDependencies(t *testing.T) {
	resolver := NewWiringResolverRecorder()
	deps := &modulebuild.Dependencies{
		Main: []modulebuild.Ref{{Kind: "external", Value: "org.example:lib:1.0"}},
		Test: []modulebuild.Ref{{Kind: "project", Value: ":test-fixture"}},
		Scoped: map[string][]modulebuild.Ref{
			"debugImplementation": {{Kind: "external", Value: "org.example:debug:1.0"}},
		},
	}

	if _, err := resolver.Resolve(deps); err != nil {
		t.Fatal(err)
	}

	deps.Main[0].Value = "mutated-main"
	deps.Test[0].Value = "mutated-test"
	deps.Scoped["debugImplementation"][0].Value = "mutated-debug"

	calls := resolver.CallsSnapshot()
	if got := calls[0].Main[0].Value; got != "org.example:lib:1.0" {
		t.Fatalf("Main[0].Value = %q", got)
	}
	if got := calls[0].Test[0].Value; got != ":test-fixture" {
		t.Fatalf("Test[0].Value = %q", got)
	}
	if got := calls[0].Scoped["debugImplementation"][0].Value; got != "org.example:debug:1.0" {
		t.Fatalf("Scoped debug value = %q", got)
	}
}

func TestWiringResolverRecorderCallsSnapshotReturnsCopies(t *testing.T) {
	resolver := NewWiringResolverRecorder()
	deps := &modulebuild.Dependencies{
		Main: []modulebuild.Ref{{Kind: "external", Value: "org.example:lib:1.0"}},
		Scoped: map[string][]modulebuild.Ref{
			"debugImplementation": {{Kind: "external", Value: "org.example:debug:1.0"}},
		},
	}

	if _, err := resolver.Resolve(deps); err != nil {
		t.Fatal(err)
	}

	calls := resolver.CallsSnapshot()
	calls[0].Main[0].Value = "mutated-main"
	calls[0].Scoped["debugImplementation"][0].Value = "mutated-debug"
	calls[0].Scoped["releaseImplementation"] = []modulebuild.Ref{{Kind: "external", Value: "org.example:release:1.0"}}

	fresh := resolver.CallsSnapshot()
	if got := fresh[0].Main[0].Value; got != "org.example:lib:1.0" {
		t.Fatalf("Main[0].Value = %q", got)
	}
	if got := fresh[0].Scoped["debugImplementation"][0].Value; got != "org.example:debug:1.0" {
		t.Fatalf("Scoped debug value = %q", got)
	}
	if _, ok := fresh[0].Scoped["releaseImplementation"]; ok {
		t.Fatalf("snapshot mutation added releaseImplementation: %#v", fresh[0].Scoped)
	}
}

func TestWiringResolverRecorderTopology(t *testing.T) {
	resolver := NewWiringResolverRecorder()
	resolver.SetTopology(m2local.CacheTopology{
		CacheRoot: "/cache",
		WorkRoot:  "/work",
	})

	topo := resolver.Topology()
	if topo.CacheRoot != "/cache" || topo.WorkRoot != "/work" {
		t.Fatalf("topology = %#v", topo)
	}
}
