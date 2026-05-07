package modulebuild

// DependencyRequest is the normalized, provenance-preserving form of a Gradle
// dependency declaration. The existing Ref lists remain as compatibility
// adapters while the resolver moves to this richer surface.
type DependencyRequest struct {
	Scope         string
	Kind          string
	Value         string
	Raw           string
	Platform      bool
	Enforced      bool
	Project       bool
	CatalogAlias  string
	BundleAlias   string
	Exclusions    []DependencyExclusion
	Capabilities  []string
	Attributes    map[string]string
	Configuration string
}

type DependencyExclusion struct {
	Group  string
	Module string
}
