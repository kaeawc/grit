package m2local

import (
	"os"
	"testing"
)

func TestResolveModuleMetadataReportsVariantSelection(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()
	resolver := New(cacheRoot, t.TempDir(), nil, nil)
	coord := Coordinate{Group: "g", Module: "m", Version: "1.0.0"}
	path := writeModuleMetadataFile(t, `{
		"variants": [
			{
				"name": "metadataApiElements",
				"attributes": {
					"org.jetbrains.kotlin.platform.type": "common",
					"org.gradle.usage": "kotlin-metadata"
				},
				"files": [{"name":"lib-metadata.jar","url":"lib-metadata.jar"}]
			},
			{
				"name": "releaseRuntimeElements",
				"attributes": {
					"org.jetbrains.kotlin.platform.type": "androidJvm",
					"org.gradle.usage": "java-runtime"
				},
				"capabilities": [
					{"group":"g","name":"m-runtime","version":"1.0.0"}
				],
				"files": [{"name":"lib-release.jar","url":"lib-release.jar"}]
			}
		]
	}`)
	writeCachedArtifact(t, resolver.moduleBasePath(coord), "lib-release.jar")

	resolver.resetReport()
	resolver.resetReplay()
	source := &ResolutionMetadataSource{
		Kind:          "module",
		Path:          path,
		RepositoryURL: "https://repo1.maven.org/maven2/",
		Fetched:       true,
	}
	_, _, _, err := resolver.resolveModuleMetadata(coord, path, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	report := resolver.snapshotReport()
	if len(report.Selections) != 3 {
		t.Fatalf("expected two selections, got %#v", report)
	}
	if report.Selections[0].Kind != "variant_selection" || report.Selections[0].Chosen != "releaseRuntimeElements" {
		t.Fatalf("unexpected selection report: %#v", report.Selections[0])
	}
	if len(report.Selections[0].Capabilities) != 1 || report.Selections[0].Capabilities[0] != "g:m-runtime:1.0.0" {
		t.Fatalf("expected variant capabilities on selection, got %#v", report.Selections[0])
	}
	if report.Selections[1].Kind != "capability_selection" || report.Selections[1].Chosen != "releaseRuntimeElements" {
		t.Fatalf("unexpected capability selection report: %#v", report.Selections[1])
	}
	if report.Selections[2].Kind != "realization_binding" || report.Selections[2].Binding != "jar" {
		t.Fatalf("unexpected realization binding report: %#v", report.Selections[2])
	}
	if report.Selections[0].MetadataSource == nil || report.Selections[0].MetadataSource.RepositoryURL == "" {
		t.Fatalf("expected metadata provenance on selection, got %#v", report.Selections[0])
	}
	replay := resolver.snapshotReplay()
	if len(replay.Pins) != 1 || replay.Pins[0].Coordinate != "g:m:1.0.0" || replay.Pins[0].Variant != "releaseRuntimeElements" {
		t.Fatalf("unexpected replay pins: %#v", replay)
	}
	if replay.Pins[0].Binding != "jar" {
		t.Fatalf("unexpected replay binding: %#v", replay.Pins[0])
	}
	if replay.Pins[0].RepositoryURL != "https://repo1.maven.org/maven2/" {
		t.Fatalf("unexpected replay repository url: %#v", replay.Pins[0])
	}
	if len(replay.Pins[0].Capabilities) != 1 || replay.Pins[0].Capabilities[0] != "g:m-runtime:1.0.0" {
		t.Fatalf("unexpected replay capabilities: %#v", replay.Pins[0])
	}
}

func TestResolveClosureReportsVersionConflict(t *testing.T) {
	t.Parallel()

	resolver := New(t.TempDir(), t.TempDir(), nil, nil)
	resolver.resetReport()
	_, _, err := resolver.resolveClosure([]Coordinate{
		{Group: "g", Module: "m", Version: "1.0.0"},
		{Group: "g", Module: "m", Version: "2.0.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	report := resolver.snapshotReport()
	if len(report.Conflicts) != 1 {
		t.Fatalf("expected one conflict, got %#v", report)
	}
	if report.Conflicts[0].Kind != "version_conflict" || report.Conflicts[0].Module != "g:m" {
		t.Fatalf("unexpected conflict report: %#v", report.Conflicts[0])
	}
}

func writeModuleMetadataFile(t *testing.T, body string) string {
	t.Helper()
	path := t.TempDir() + "/module.module"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeCachedArtifact(t *testing.T, base, name string) {
	t.Helper()
	hashDir := base + "/hash"
	if err := os.MkdirAll(hashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hashDir+"/"+name, []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
}
