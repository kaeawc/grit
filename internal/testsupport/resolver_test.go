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
