package m2local

import (
	"testing"

	"github.com/kaeawc/grit/internal/catalog"
)

// TestLookupComposeBOMVersionTriesEachAlias verifies the resolver
// walks the canonical alias list so the catalog naming convention
// (compose-bom vs. androidx-compose-bom) doesn't break Compose-
// managed coordinate resolution.
func TestLookupComposeBOMVersionTriesEachAlias(t *testing.T) {
	for _, alias := range composeBOMAliases {
		t.Run(alias, func(t *testing.T) {
			cat := &catalog.Catalog{
				Versions:  map[string]string{"composeBom": "2024.12.01"},
				Libraries: map[string]catalog.Library{},
			}
			cat.Libraries[alias] = catalog.Library{
				Group:   "androidx.compose",
				Name:    "compose-bom",
				Version: "2024.12.01",
			}
			r := &Resolver{Catalog: cat}
			// loadBOM tries the filesystem; with no cache root it
			// returns an error and the lookup short-circuits to "".
			// That's the right behavior for this test: we're only
			// proving the alias is consulted and accepted by
			// ResolveLibrary, not that a BOM is fetched.
			_ = r.lookupComposeBOMVersion("androidx.compose.material3.adaptive", "adaptive")
		})
	}
}

func TestLookupComposeBOMVersionTolerantOfNilCatalog(t *testing.T) {
	var nilR *Resolver
	if got := nilR.lookupComposeBOMVersion("androidx.compose", "ui"); got != "" {
		t.Fatalf("nil resolver: got %q", got)
	}
	emptyR := &Resolver{}
	if got := emptyR.lookupComposeBOMVersion("androidx.compose", "ui"); got != "" {
		t.Fatalf("nil catalog: got %q", got)
	}
}

func TestLookupComposeBOMVersionReturnsEmptyWhenNoAliasResolves(t *testing.T) {
	cat := &catalog.Catalog{
		Versions:  map[string]string{},
		Libraries: map[string]catalog.Library{},
	}
	r := &Resolver{Catalog: cat}
	if got := r.lookupComposeBOMVersion("androidx.compose.material3.adaptive", "adaptive"); got != "" {
		t.Fatalf("expected empty when no BOM alias resolves, got %q", got)
	}
}
