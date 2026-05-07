package nativecompile

import (
	"testing"

	"github.com/kaeawc/grit/internal/catalog"
	"github.com/kaeawc/grit/internal/gradlecache"
	"github.com/kaeawc/grit/internal/modulebuild"
)

func TestJUnitJupiterVersionForPlatform(t *testing.T) {
	t.Parallel()

	tests := []struct {
		platform string
		want     string
	}{
		{platform: "1.12.2", want: "5.12.2"},
		{platform: "1.9.2", want: "5.9.2"},
		{platform: "6.0.3", want: "6.0.3"},
		{platform: "bad", want: ""},
	}
	for _, tt := range tests {
		if got := junitJupiterVersionForPlatform(tt.platform); got != tt.want {
			t.Fatalf("junitJupiterVersionForPlatform(%q) = %q, want %q", tt.platform, got, tt.want)
		}
	}
}

func TestJUnitPlatformVersionForJupiter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		jupiter string
		want    string
	}{
		{jupiter: "5.12.2", want: "1.12.2"},
		{jupiter: "6.0.3", want: "6.0.3"},
		{jupiter: "bad", want: ""},
	}
	for _, tt := range tests {
		if got := junitPlatformVersionForJupiter(tt.jupiter); got != tt.want {
			t.Fatalf("junitPlatformVersionForJupiter(%q) = %q, want %q", tt.jupiter, got, tt.want)
		}
	}
}

func TestAlignedJUnitRuntimeVersionsInferFromCatalogDependencyAndMetadata(t *testing.T) {
	t.Parallel()

	cache := fakeJUnitArtifactCache{
		deps: map[string][]gradlecache.Dependency{
			"org.junit.platform:junit-platform-launcher:1.10.2": {
				{Group: "org.junit.platform", Module: "junit-platform-engine", Version: "1.10.2"},
				{Group: "org.junit.platform", Module: "junit-platform-commons", Version: "1.10.2"},
				{Group: "org.apiguardian", Module: "apiguardian-api", Version: "1.1.2"},
				{Group: "org.opentest4j", Module: "opentest4j", Version: "1.3.0"},
			},
			"org.junit.jupiter:junit-jupiter-engine:5.10.2": {
				{Group: "org.junit.jupiter", Module: "junit-jupiter-api", Version: "5.10.2"},
				{Group: "org.junit.platform", Module: "junit-platform-engine", Version: "1.10.2"},
			},
		},
	}
	deps := &modulebuild.Dependencies{
		Test: []modulebuild.Ref{{Kind: "library", Value: "junit.jupiter.engine"}},
	}
	cat := &catalog.Catalog{
		Versions: map[string]string{"junit": "5.10.2"},
		Libraries: map[string]catalog.Library{
			"junit-jupiter-engine": {
				Group: "org.junit.jupiter", Name: "junit-jupiter-engine", VersionRef: "junit",
			},
		},
	}

	got := alignedJUnitRuntimeVersionsWith(cache, deps, cat)
	if got.platform != "1.10.2" || got.jupiter != "5.10.2" {
		t.Fatalf("unexpected versions: %#v", got)
	}
	if got.deps["org.apiguardian:apiguardian-api"] != "1.1.2" || got.deps["org.opentest4j:opentest4j"] != "1.3.0" {
		t.Fatalf("expected metadata dependency versions, got %#v", got.deps)
	}
}

type fakeJUnitArtifactCache struct {
	versions map[string][]string
	jars     map[string]string
	deps     map[string][]gradlecache.Dependency
}

func (f fakeJUnitArtifactCache) Versions(group, module string) []string {
	return append([]string(nil), f.versions[group+":"+module]...)
}

func (f fakeJUnitArtifactCache) Jar(group, module, version string) string {
	if f.jars == nil {
		return "/fake/" + group + "/" + module + "/" + version + ".jar"
	}
	return f.jars[group+":"+module+":"+version]
}

func (f fakeJUnitArtifactCache) Dependencies(group, module, version string) []gradlecache.Dependency {
	return append([]gradlecache.Dependency(nil), f.deps[group+":"+module+":"+version]...)
}
