package mavenlocal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/lockfile"
)

type testPublishStore struct {
	ctx      context.Context
	store    *cas.FilesystemStore
	hashes   map[string]cas.Hash
	payloads map[string][]byte
}

func newTestPublishStore(t *testing.T, root string, payloads map[string][]byte) testPublishStore {
	t.Helper()
	store := cas.NewFilesystemStore(root)
	ctx := context.Background()
	hashes := map[string]cas.Hash{}
	for name, payload := range payloads {
		info, err := store.PutBytes(ctx, payload, cas.Provenance{})
		if err != nil {
			t.Fatalf("PutBytes %s: %v", name, err)
		}
		hashes[name] = info.Hash
	}
	return testPublishStore{
		ctx:      ctx,
		store:    store,
		hashes:   hashes,
		payloads: payloads,
	}
}

func TestGeneratedModulePayloadPreservesAttributesCapabilitiesAndDependencies(t *testing.T) {
	payload, ok, err := generatedModulePayload(lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.2.3"},
		Attributes: map[string]string{
			"org.gradle.usage": "java-api",
		},
		Capabilities: []string{"org.example:demo-feature:1.2.3"},
		Dependencies: []lockfile.Coordinate{
			{Group: "org.dep", Artifact: "alpha", Version: "2.0.0"},
		},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "demo-1.2.3.jar"},
			{Kind: lockfile.FileKindSources, Name: "demo-1.2.3-sources.jar"},
			{Kind: lockfile.FileKindJavadoc, Name: "demo-1.2.3-javadoc.jar"},
		},
	})
	if err != nil {
		t.Fatalf("generatedModulePayload: %v", err)
	}
	if !ok {
		t.Fatalf("expected generated module payload")
	}

	var got gradleModuleMetadata
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.FormatVersion != "1.1" {
		t.Fatalf("unexpected format version: %#v", got)
	}
	if got.Component.Group != "org.example" || got.Component.Module != "demo" || got.Component.Version != "1.2.3" {
		t.Fatalf("unexpected component: %#v", got.Component)
	}
	if len(got.Variants) != 3 {
		t.Fatalf("expected runtime + docs variants, got %#v", got.Variants)
	}
	runtime := got.Variants[0]
	if runtime.Name != "apiElements" {
		t.Fatalf("unexpected runtime variant name: %#v", runtime)
	}
	if runtime.Attributes["org.gradle.usage"] != "java-api" || runtime.Attributes["org.jetbrains.kotlin.platform.type"] != "jvm" {
		t.Fatalf("unexpected runtime attrs: %#v", runtime.Attributes)
	}
	if len(runtime.Capabilities) != 1 || runtime.Capabilities[0].Name != "demo-feature" {
		t.Fatalf("unexpected capabilities: %#v", runtime.Capabilities)
	}
	if len(runtime.Dependencies) != 1 || runtime.Dependencies[0].Module != "alpha" || runtime.Dependencies[0].Version.Requires != "2.0.0" {
		t.Fatalf("unexpected dependencies: %#v", runtime.Dependencies)
	}
	if len(runtime.Files) != 1 || runtime.Files[0].URL != "demo-1.2.3.jar" {
		t.Fatalf("unexpected runtime files: %#v", runtime.Files)
	}
	if got.Variants[1].Attributes["org.gradle.docstype"] != "sources" || got.Variants[2].Attributes["org.gradle.docstype"] != "javadoc" {
		t.Fatalf("unexpected documentation variants: %#v", got.Variants)
	}
}

func TestGeneratedModulePayloadSkipsPinsWithoutPrimaryArtifacts(t *testing.T) {
	_, ok, err := generatedModulePayload(lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.0.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPOM, Name: "demo-1.0.0.pom"},
		},
	})
	if err != nil {
		t.Fatalf("generatedModulePayload: %v", err)
	}
	if ok {
		t.Fatalf("expected no generated module without primary artifact")
	}
}

func TestPublishPinPreservesExplicitModuleFile(t *testing.T) {
	root := t.TempDir()
	casRoot := t.TempDir()
	store := newTestPublishStore(t, casRoot, map[string][]byte{
		"demo-1.0.0.jar":    []byte("jar"),
		"demo-1.0.0.module": []byte(`{"variants":[{"name":"releaseRuntimeElements","attributes":{"org.gradle.usage":"java-runtime"},"files":[{"url":"demo-1.0.0.jar"}]}]}`),
	})
	p := New(root)
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.0.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "demo-1.0.0.jar", Hash: store.hashes["demo-1.0.0.jar"]},
			{Kind: lockfile.FileKindModule, Name: "demo-1.0.0.module", Hash: store.hashes["demo-1.0.0.module"]},
		},
	}
	if err := p.PublishPin(store.ctx, pin, store.store); err != nil {
		t.Fatalf("PublishPin: %v", err)
	}
	modulePath := filepath.Join(root, "org", "example", "demo", "1.0.0", "demo-1.0.0.module")
	got, err := os.ReadFile(modulePath)
	if err != nil {
		t.Fatalf("ReadFile module: %v", err)
	}
	if string(got) != string(store.payloads["demo-1.0.0.module"]) {
		t.Fatalf("expected explicit module bytes to be preserved, got %q", got)
	}
}

func TestGeneratedModuleCapabilitiesIgnoreMalformedEntries(t *testing.T) {
	caps := gradleModuleCapabilities(lockfile.Pin{Capabilities: []string{"bad", "a:b:1.0.0"}})
	if len(caps) != 1 || caps[0].Group != "a" || caps[0].Name != "b" || caps[0].Version != "1.0.0" {
		t.Fatalf("unexpected capabilities: %#v", caps)
	}
}

func TestGradleModuleFilesForKindsRetainsDeclaredOrder(t *testing.T) {
	files := gradleModuleFilesForKinds([]lockfile.PinFile{
		{Kind: lockfile.FileKindSources, Name: "a-sources.jar"},
		{Kind: lockfile.FileKindSources, Name: "b-sources.jar"},
	}, lockfile.FileKindSources)
	if !slices.Equal([]gradleModuleFile{{Name: "a-sources.jar", URL: "a-sources.jar"}, {Name: "b-sources.jar", URL: "b-sources.jar"}}, files) {
		t.Fatalf("unexpected files: %#v", files)
	}
}
