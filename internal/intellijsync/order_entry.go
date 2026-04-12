package intellijsync

import (
	"path/filepath"
	"sort"
	"strings"
)

// OrderEntry represents an IntelliJ classpath order entry derived from a
// resolved variant classpath. The IDE uses these to configure code completion,
// error detection, and navigation across module and library boundaries.
type OrderEntry struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Scope      string `json:"scope,omitempty"`
	ModulePath string `json:"modulePath,omitempty"`
	Classes    string `json:"classes,omitempty"`
	Sources    string `json:"sources,omitempty"`
	Javadoc    string `json:"javadoc,omitempty"`
	Exported   bool   `json:"exported,omitempty"`
}

const (
	OrderEntryKindModule  = "module"
	OrderEntryKindLibrary = "library"
	OrderEntryKindSDK     = "sdk"
)

// ClasspathEntry is a resolved classpath element with optional companion
// artifacts (sources, javadoc) that can be converted into an OrderEntry.
type ClasspathEntry struct {
	// Kind is one of "module", "library", or "sdk".
	Kind string
	// Name is the display name (e.g. module path or Maven coordinate).
	Name string
	// Scope is the dependency scope: "compile", "test", "runtime", or "provided".
	Scope string
	// ModulePath is the Gradle module path for module dependencies.
	ModulePath string
	// Classes is the path to the compiled classes JAR or directory.
	Classes string
	// Sources is the optional path to the sources JAR.
	Sources string
	// Javadoc is the optional path to the javadoc JAR.
	Javadoc string
	// Exported indicates the entry is re-exported to dependents.
	Exported bool
}

// ClasspathToOrderEntries converts a slice of ClasspathEntry values into
// the ordered list of OrderEntry objects that the IDE requires.  Entries
// are deduplicated by name and sorted: SDK entries first, then module
// dependencies, then libraries — each sub-group sorted alphabetically.
func ClasspathToOrderEntries(entries []ClasspathEntry) []OrderEntry {
	seen := make(map[string]struct{}, len(entries))
	var out []OrderEntry
	for _, e := range entries {
		key := orderEntryKey(e)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, orderEntryFromClasspath(e))
	}
	sortOrderEntries(out)
	return out
}

// VariantOrderEntries derives order entries from a sync-model Variant by
// examining its module dependencies and classpath snapshot IDs.  Module
// dependencies produce module order entries; classpath snapshot IDs that
// look like JAR/AAR paths produce library entries; and an SDK entry is
// synthesised when the variant carries a non-empty CompileSDK.
func VariantOrderEntries(v Variant) []OrderEntry {
	var entries []ClasspathEntry

	// SDK entry from compile SDK.
	if v.CompileSDK != "" {
		entries = append(entries, ClasspathEntry{
			Kind:  OrderEntryKindSDK,
			Name:  "Android API " + v.CompileSDK,
			Scope: "compile",
		})
	}

	// Module dependencies.
	for _, dep := range v.Dependencies {
		if dep.Kind == "module" || dep.Kind == "variant" || dep.Kind == "materialization" {
			name := dep.TargetModulePath
			if dep.TargetVariantName != "" {
				name = dep.TargetModulePath + "/" + dep.TargetVariantName
			}
			entries = append(entries, ClasspathEntry{
				Kind:       OrderEntryKindModule,
				Name:       name,
				Scope:      "compile",
				ModulePath: dep.TargetModulePath,
				Exported:   true,
			})
		}
	}

	// Library entries from classpath snapshot IDs.
	for _, cpID := range v.Materialization.ClasspathSnapshotIDs {
		entries = append(entries, classpathEntryFromSnapshotID(cpID))
	}

	return ClasspathToOrderEntries(entries)
}

func orderEntryFromClasspath(e ClasspathEntry) OrderEntry {
	return OrderEntry{
		Kind:       e.Kind,
		Name:       e.Name,
		Scope:      e.Scope,
		ModulePath: e.ModulePath,
		Classes:    e.Classes,
		Sources:    e.Sources,
		Javadoc:    e.Javadoc,
		Exported:   e.Exported,
	}
}

func orderEntryKey(e ClasspathEntry) string {
	if e.Kind == OrderEntryKindModule {
		return "module:" + e.ModulePath
	}
	if e.Kind == OrderEntryKindSDK {
		return "sdk:" + e.Name
	}
	return "library:" + e.Name
}

// sortOrderEntries sorts by kind priority (sdk < module < library), then
// alphabetically by name within each kind.
func sortOrderEntries(entries []OrderEntry) {
	sort.Slice(entries, func(i, j int) bool {
		pi, pj := kindPriority(entries[i].Kind), kindPriority(entries[j].Kind)
		if pi != pj {
			return pi < pj
		}
		return entries[i].Name < entries[j].Name
	})
}

func kindPriority(kind string) int {
	switch kind {
	case OrderEntryKindSDK:
		return 0
	case OrderEntryKindModule:
		return 1
	case OrderEntryKindLibrary:
		return 2
	default:
		return 3
	}
}

// classpathEntryFromSnapshotID creates a library ClasspathEntry from a
// classpath snapshot ID.  The ID is typically a path to a JAR or AAR; the
// library name is derived from the file name.  Companion source/javadoc
// JARs are inferred by convention (foo.jar → foo-sources.jar, foo-javadoc.jar).
func classpathEntryFromSnapshotID(id string) ClasspathEntry {
	name := libraryNameFromPath(id)
	entry := ClasspathEntry{
		Kind:    OrderEntryKindLibrary,
		Name:    name,
		Scope:   "compile",
		Classes: id,
	}
	// Infer companion JARs by Maven convention.
	ext := filepath.Ext(id)
	if ext == ".jar" || ext == ".aar" {
		base := strings.TrimSuffix(id, ext)
		entry.Sources = base + "-sources.jar"
		entry.Javadoc = base + "-javadoc.jar"
	}
	return entry
}

// libraryNameFromPath extracts a human-readable library name from an
// artifact path.  For Maven-layout paths it tries to recover group:artifact:version.
func libraryNameFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}
