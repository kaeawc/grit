package m2local

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveClosureRootPinBlocksTransitiveUpgrade pins down the
// Gradle-compatible default conflict policy: a coordinate declared
// directly by the project (a "root") wins over higher versions that
// arrive via transitive dependencies.
//
// Concretely: project declares g:a:1.0.0 and g:b:1.0.0 directly; b 1.0.0
// transitively requires g:a:2.0.0. Without root-pinning, grit's previous
// "highest wins" policy bumped g:a to 2.0.0 — fine in isolation, but in
// practice that pulled in newer Kotlin-metadata jars (e.g. coroutines
// 1.11.0) that the project's pinned Kotlin compiler couldn't read.
func TestResolveClosureRootPinBlocksTransitiveUpgrade(t *testing.T) {
	cacheRoot := t.TempDir()
	resolver := New(cacheRoot, t.TempDir(), nil, nil)

	// g:a:1.0.0 with no deps.
	writeFixtureModuleAndJar(t, cacheRoot, "g", "a", "1.0.0", nil)
	// g:a:2.0.0 with no deps (the higher version a transitive wants).
	writeFixtureModuleAndJar(t, cacheRoot, "g", "a", "2.0.0", nil)
	// g:b:1.0.0 depends transitively on g:a:2.0.0.
	writeFixtureModuleAndJar(t, cacheRoot, "g", "b", "1.0.0", []dep{{"g", "a", "2.0.0"}})

	resolver.resetReport()
	jars, _, err := resolver.resolveClosure([]Coordinate{
		{Group: "g", Module: "a", Version: "1.0.0"},
		{Group: "g", Module: "b", Version: "1.0.0"},
	})
	if err != nil {
		t.Fatalf("resolveClosure: %v", err)
	}

	if !containsJar(jars, "a-1.0.0.jar") {
		t.Fatalf("expected a-1.0.0.jar in selected jars (root pin), got %v", jars)
	}
	if containsJar(jars, "a-2.0.0.jar") {
		t.Fatalf("a-2.0.0.jar must NOT be selected when a-1.0.0 is a root; got %v", jars)
	}

	report := resolver.snapshotReport()
	var rootPin *ResolutionConflict
	for i := range report.Conflicts {
		c := &report.Conflicts[i]
		if c.Module == "g:a" && c.Reason == "root_pin" {
			rootPin = c
			break
		}
	}
	if rootPin == nil {
		t.Fatalf("expected a root_pin conflict for g:a, got conflicts: %#v", report.Conflicts)
	}
	if rootPin.Selected != "1.0.0" || rootPin.Discarded != "2.0.0" {
		t.Fatalf("unexpected root_pin conflict: %#v", rootPin)
	}
}

// TestResolveClosureHighestWinsForTransitiveOnlyVersions keeps the
// previous "highest wins" semantics when NO root pins the module: two
// transitive arrivals at different versions still pick the higher one.
func TestResolveClosureHighestWinsForTransitiveOnlyVersions(t *testing.T) {
	cacheRoot := t.TempDir()
	resolver := New(cacheRoot, t.TempDir(), nil, nil)

	writeFixtureModuleAndJar(t, cacheRoot, "g", "a", "1.0.0", nil)
	writeFixtureModuleAndJar(t, cacheRoot, "g", "a", "2.0.0", nil)
	// b 1.0.0 transitively wants a:1.0.0; c 1.0.0 transitively wants a:2.0.0.
	writeFixtureModuleAndJar(t, cacheRoot, "g", "b", "1.0.0", []dep{{"g", "a", "1.0.0"}})
	writeFixtureModuleAndJar(t, cacheRoot, "g", "c", "1.0.0", []dep{{"g", "a", "2.0.0"}})

	resolver.resetReport()
	jars, _, err := resolver.resolveClosure([]Coordinate{
		{Group: "g", Module: "b", Version: "1.0.0"},
		{Group: "g", Module: "c", Version: "1.0.0"},
	})
	if err != nil {
		t.Fatalf("resolveClosure: %v", err)
	}

	if containsJar(jars, "a-1.0.0.jar") {
		t.Fatalf("a-1.0.0.jar must NOT be selected when no root pins g:a, got %v", jars)
	}
	if !containsJar(jars, "a-2.0.0.jar") {
		t.Fatalf("expected a-2.0.0.jar in selected jars (highest wins), got %v", jars)
	}
}

type dep struct{ group, module, version string }

func writeFixtureModuleAndJar(t *testing.T, cacheRoot, group, module, version string, deps []dep) {
	t.Helper()
	base := filepath.Join(cacheRoot, group, module, version)
	hashDir := filepath.Join(base, "hash")
	if err := os.MkdirAll(hashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	jarName := fmt.Sprintf("%s-%s.jar", module, version)
	if err := os.WriteFile(filepath.Join(hashDir, jarName), []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	moduleName := fmt.Sprintf("%s-%s.module", module, version)
	if err := os.WriteFile(filepath.Join(hashDir, moduleName), []byte(buildModuleJSON(jarName, deps)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func buildModuleJSON(jarName string, deps []dep) string {
	var depJSON []string
	for _, d := range deps {
		depJSON = append(depJSON, fmt.Sprintf(
			`{"group":"%s","module":"%s","version":{"requires":"%s"}}`,
			d.group, d.module, d.version,
		))
	}
	return fmt.Sprintf(`{
		"variants": [
			{
				"name": "runtimeElements",
				"attributes": {
					"org.jetbrains.kotlin.platform.type": "jvm",
					"org.gradle.usage": "java-runtime"
				},
				"files": [{"name":"%s","url":"%s"}],
				"dependencies": [%s]
			}
		]
	}`, jarName, jarName, strings.Join(depJSON, ","))
}

func containsJar(jars []string, name string) bool {
	for _, j := range jars {
		if strings.HasSuffix(j, "/"+name) || strings.HasSuffix(j, name) {
			return true
		}
	}
	return false
}
