package m2local

import (
	"testing"

	"github.com/kaeawc/grit/internal/catalog"
)

func TestInferAlignedVersionForKotlinFollowsCatalogKotlinKey(t *testing.T) {
	cases := []struct {
		name     string
		versions map[string]string
		want     string
	}{
		{"primary kotlin key", map[string]string{"kotlin": "2.0.21"}, "2.0.21"},
		{"build-kotlin fallback", map[string]string{"build-kotlin": "2.1.0"}, "2.1.0"},
		{"kotlin-version fallback", map[string]string{"kotlin-version": "2.2.0"}, "2.2.0"},
		{"primary wins over fallback", map[string]string{"kotlin": "2.0.21", "build-kotlin": "1.9.0"}, "2.0.21"},
		{"none configured", map[string]string{"androidGradlePlugin": "8.5.0"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Resolver{Catalog: &catalog.Catalog{Versions: tc.versions}}
			if got := r.inferAlignedVersion("org.jetbrains.kotlin"); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestInferAlignedVersionIgnoresUnknownGroups(t *testing.T) {
	r := &Resolver{Catalog: &catalog.Catalog{Versions: map[string]string{"kotlin": "2.0.21"}}}
	if got := r.inferAlignedVersion("com.squareup.okhttp3"); got != "" {
		t.Fatalf("expected empty for non-kotlin group, got %q", got)
	}
}

func TestInferAlignedVersionToleratesNilResolverAndCatalog(t *testing.T) {
	var nilResolver *Resolver
	if got := nilResolver.inferAlignedVersion("org.jetbrains.kotlin"); got != "" {
		t.Fatalf("nil receiver should return empty, got %q", got)
	}
	emptyResolver := &Resolver{}
	if got := emptyResolver.inferAlignedVersion("org.jetbrains.kotlin"); got != "" {
		t.Fatalf("nil catalog should return empty, got %q", got)
	}
}
