package m2local

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeVersion(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"1.2.3":       "1.2.3",
		"[1.2.3]":     "1.2.3",
		"(1.2.3,2.0)": "1.2.3",
		" [1.0, ) ":   "1.0",
	}
	for in, want := range tests {
		if got := normalizeVersion(in); got != want {
			t.Fatalf("normalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChooseVariantPrefersAndroidRuntime(t *testing.T) {
	t.Parallel()

	variants := []moduleVariant{
		{
			Name: "jvmApiElements",
			Attributes: map[string]string{
				"org.gradle.usage": "java-api",
			},
			Files: []moduleFile{{URL: "lib.jar"}},
		},
		{
			Name: "releaseRuntimeElements",
			Attributes: map[string]string{
				"org.jetbrains.kotlin.platform.type": "androidJvm",
				"org.gradle.usage":                   "java-runtime",
			},
			Files: []moduleFile{{URL: "lib-release.aar"}},
		},
	}

	chosen := chooseVariant(variants)
	if chosen == nil {
		t.Fatal("expected a chosen variant")
	}
	if chosen.Name != "releaseRuntimeElements" {
		t.Fatalf("unexpected chosen variant: %#v", chosen.Name)
	}
}

func TestChooseVariantPrefersJVMOverCommonMetadata(t *testing.T) {
	t.Parallel()

	variants := []moduleVariant{
		{
			Name: "metadataApiElements",
			Attributes: map[string]string{
				"org.jetbrains.kotlin.platform.type": "common",
				"org.gradle.usage":                   "kotlin-metadata",
			},
			Files: []moduleFile{{URL: "lib-metadata.jar"}},
		},
		{
			Name: "jvmRuntimeElements",
			Attributes: map[string]string{
				"org.jetbrains.kotlin.platform.type": "jvm",
				"org.gradle.usage":                   "java-runtime",
			},
			Files: []moduleFile{{URL: "lib-jvm.jar"}},
		},
	}

	chosen := chooseVariant(variants)
	if chosen == nil {
		t.Fatal("expected a chosen variant")
	}
	if chosen.Name != "jvmRuntimeElements" {
		t.Fatalf("unexpected chosen variant: %#v", chosen.Name)
	}
}

func TestToCoordinatesWithConstraintsUsesConstraintVersionOnlyForMissingVersion(t *testing.T) {
	t.Parallel()

	deps := []moduleDep{
		{Group: "a", Module: "x"},
		{Group: "b", Module: "y", Version: struct {
			Requires string `json:"requires"`
		}{Requires: "2.0.0"}},
	}
	constraints := map[string]string{
		"a:x": "1.0.0",
		"c:z": "3.0.0",
	}

	got := toCoordinatesWithConstraints(deps, constraints)
	if len(got) != 2 {
		t.Fatalf("unexpected coordinate count: %#v", got)
	}
	if got[0].Group != "a" || got[0].Module != "x" || got[0].Version != "1.0.0" {
		t.Fatalf("unexpected first coordinate: %#v", got[0])
	}
	if got[1].Group != "b" || got[1].Module != "y" || got[1].Version != "2.0.0" {
		t.Fatalf("unexpected second coordinate: %#v", got[1])
	}
}

func TestConstraintVersionsDoesNotCreateDependenciesByItself(t *testing.T) {
	t.Parallel()

	constraints := []moduleDep{
		{Group: "legacy", Module: "support-compat", Version: struct {
			Requires string `json:"requires"`
		}{Requires: "26.1.0"}},
	}

	got := toCoordinatesWithConstraints(nil, constraintVersions(constraints))
	if len(got) != 0 {
		t.Fatalf("constraints should not become direct dependencies: %#v", got)
	}
}

func TestToCoordinatesWithConstraintsCarriesExcludes(t *testing.T) {
	t.Parallel()

	deps := []moduleDep{
		{
			Group:  "com.google.android.gms",
			Module: "play-services-tasks",
			Excludes: []moduleExclude{
				{Group: "com.android.support", Module: "*"},
			},
			Version: struct {
				Requires string `json:"requires"`
			}{Requires: "16.0.1"},
		},
	}

	got := toCoordinatesWithConstraints(deps, nil)
	if len(got) != 1 {
		t.Fatalf("unexpected coordinate count: %#v", got)
	}
	if len(got[0].Excludes) != 1 || got[0].Excludes[0] != (Exclude{Group: "com.android.support", Module: "*"}) {
		t.Fatalf("unexpected excludes: %#v", got[0].Excludes)
	}
}

func TestParsePOMDepsCarriesExclusions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/demo.pom"
	body := `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <dependencies>
    <dependency>
      <groupId>com.google.android.gms</groupId>
      <artifactId>play-services-tasks</artifactId>
      <version>16.0.1</version>
      <scope>compile</scope>
      <exclusions>
        <exclusion>
          <groupId>com.android.support</groupId>
          <artifactId>*</artifactId>
        </exclusion>
      </exclusions>
    </dependency>
  </dependencies>
</project>`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write pom: %v", err)
	}

	got, err := parsePOMDeps(path)
	if err != nil {
		t.Fatalf("parse pom deps: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("unexpected deps: %#v", got)
	}
	if len(got[0].Excludes) != 1 || got[0].Excludes[0] != (Exclude{Group: "com.android.support", Module: "*"}) {
		t.Fatalf("unexpected pom excludes: %#v", got[0].Excludes)
	}
}

func TestParsePOMDepsResolvesProjectVersionProperty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/demo.pom"
	body := `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>2.57.2</version>
  <dependencies>
    <dependency>
      <groupId>com.google.dagger</groupId>
      <artifactId>hilt-core</artifactId>
      <version>${project.version}</version>
    </dependency>
  </dependencies>
</project>`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write pom: %v", err)
	}

	got, err := parsePOMDeps(path)
	if err != nil {
		t.Fatalf("parse pom deps: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("unexpected deps: %#v", got)
	}
	if got[0].Version != "2.57.2" {
		t.Fatalf("unexpected resolved version: %#v", got[0])
	}
}

func TestChooseVariantSelectsAvailableAtVariant(t *testing.T) {
	t.Parallel()

	variants := []moduleVariant{
		{
			Name: "jvmApiElements-published",
			Attributes: map[string]string{
				"org.jetbrains.kotlin.platform.type": "jvm",
				"org.gradle.usage":                   "java-api",
			},
			AvailableAt: &moduleAvailableAt{
				Group:   "org.example",
				Module:  "lib-jvm",
				Version: "1.0.0",
			},
		},
		{
			Name: "jvmRuntimeElements-published",
			Attributes: map[string]string{
				"org.jetbrains.kotlin.platform.type": "jvm",
				"org.gradle.usage":                   "java-runtime",
			},
			AvailableAt: &moduleAvailableAt{
				Group:   "org.example",
				Module:  "lib-jvm",
				Version: "1.0.0",
			},
		},
	}

	chosen := chooseVariant(variants)
	if chosen == nil {
		t.Fatal("expected a chosen variant")
	}
	if chosen.AvailableAt == nil {
		t.Fatal("expected chosen variant to have available-at")
	}
	if chosen.AvailableAt.Module != "lib-jvm" {
		t.Fatalf("expected available-at module lib-jvm, got %s", chosen.AvailableAt.Module)
	}
}

func TestResolveOneFollowsAvailableAtRedirect(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()
	resolver := New(cacheRoot, t.TempDir(), nil, nil)

	// Set up the root module with an available-at redirect.
	rootCoord := Coordinate{Group: "org.example", Module: "lib", Version: "1.0.0"}
	rootBase := resolver.moduleBasePath(rootCoord)
	rootHashDir := rootBase + "/hash"
	if err := os.MkdirAll(rootHashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rootModule := `{
		"variants": [
			{
				"name": "jvmRuntimeElements-published",
				"attributes": {
					"org.jetbrains.kotlin.platform.type": "jvm",
					"org.gradle.usage": "java-runtime"
				},
				"available-at": {
					"group": "org.example",
					"module": "lib-jvm",
					"version": "1.0.0"
				}
			}
		]
	}`
	if err := os.WriteFile(rootHashDir+"/lib-1.0.0.module", []byte(rootModule), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up the target module that the redirect points to.
	targetCoord := Coordinate{Group: "org.example", Module: "lib-jvm", Version: "1.0.0"}
	targetBase := resolver.moduleBasePath(targetCoord)
	targetHashDir := targetBase + "/hash"
	if err := os.MkdirAll(targetHashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	targetModule := `{
		"variants": [
			{
				"name": "jvmRuntimeElements",
				"attributes": {
					"org.jetbrains.kotlin.platform.type": "jvm",
					"org.gradle.usage": "java-runtime"
				},
				"files": [{"name":"lib-jvm-1.0.0.jar","url":"lib-jvm-1.0.0.jar"}]
			}
		]
	}`
	if err := os.WriteFile(targetHashDir+"/lib-jvm-1.0.0.module", []byte(targetModule), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetHashDir+"/lib-jvm-1.0.0.jar", []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolver.resetReport()
	resolver.resetReplay()
	artifact, _, _, err := resolver.resolveOne(rootCoord)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(artifact) || !strings.HasSuffix(artifact, "lib-jvm-1.0.0.jar") {
		t.Fatalf("expected artifact path ending in lib-jvm-1.0.0.jar, got %s", artifact)
	}

	report := resolver.snapshotReport()
	var found bool
	for _, s := range report.Selections {
		if s.Kind == "available_at_redirect" {
			found = true
			if s.Chosen != "org.example:lib-jvm:1.0.0" {
				t.Fatalf("unexpected redirect target: %s", s.Chosen)
			}
		}
	}
	if !found {
		t.Fatal("expected an available_at_redirect selection in the report")
	}
}

func TestResolveOneRejectsDeepAvailableAtChain(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()
	resolver := New(cacheRoot, t.TempDir(), nil, nil)

	// Create a chain of available-at redirects deeper than maxAvailableAtDepth.
	for i := 0; i <= maxAvailableAtDepth+1; i++ {
		mod := fmt.Sprintf("lib-%d", i)
		next := fmt.Sprintf("lib-%d", i+1)
		coord := Coordinate{Group: "org.example", Module: mod, Version: "1.0.0"}
		base := resolver.moduleBasePath(coord)
		hashDir := base + "/hash"
		if err := os.MkdirAll(hashDir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf(`{
			"variants": [{
				"name": "jvmRuntimeElements",
				"attributes": {
					"org.jetbrains.kotlin.platform.type": "jvm",
					"org.gradle.usage": "java-runtime"
				},
				"available-at": {
					"group": "org.example",
					"module": "%s",
					"version": "1.0.0"
				}
			}]
		}`, next)
		if err := os.WriteFile(hashDir+"/"+mod+"-1.0.0.module", []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	resolver.resetReport()
	_, _, _, err := resolver.resolveOne(Coordinate{Group: "org.example", Module: "lib-0", Version: "1.0.0"})
	if err == nil {
		t.Fatal("expected an error for excessive available-at redirect depth")
	}
	if !strings.Contains(err.Error(), "redirect depth exceeded") {
		t.Fatalf("expected redirect depth error, got: %v", err)
	}
}

func TestParseBOMExtractsManagedVersions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "compose-bom-2024.06.00.pom")
	body := `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>androidx.compose</groupId>
  <artifactId>compose-bom</artifactId>
  <version>2024.06.00</version>
  <packaging>pom</packaging>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>androidx.compose.ui</groupId>
        <artifactId>ui</artifactId>
        <version>1.6.8</version>
      </dependency>
      <dependency>
        <groupId>androidx.compose.material3</groupId>
        <artifactId>material3</artifactId>
        <version>1.2.1</version>
      </dependency>
      <dependency>
        <groupId>androidx.compose.foundation</groupId>
        <artifactId>foundation</artifactId>
        <version>1.6.8</version>
      </dependency>
      <dependency>
        <groupId>androidx.compose.runtime</groupId>
        <artifactId>runtime</artifactId>
        <version>1.6.8</version>
      </dependency>
    </dependencies>
  </dependencyManagement>
</project>`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write pom: %v", err)
	}

	got, err := parseBOM(path)
	if err != nil {
		t.Fatalf("parseBOM: %v", err)
	}

	expected := map[string]string{
		"androidx.compose.ui:ui":                   "1.6.8",
		"androidx.compose.material3:material3":     "1.2.1",
		"androidx.compose.foundation:foundation":   "1.6.8",
		"androidx.compose.runtime:runtime":         "1.6.8",
	}
	if len(got) != len(expected) {
		t.Fatalf("expected %d managed versions, got %d: %v", len(expected), len(got), got)
	}
	for key, want := range expected {
		if got[key] != want {
			t.Fatalf("managed version for %s: got %q, want %q", key, got[key], want)
		}
	}
}

func TestParseBOMResolvesPropertyVersions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "firebase-bom-33.1.0.pom")
	body := `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.google.firebase</groupId>
  <artifactId>firebase-bom</artifactId>
  <version>33.1.0</version>
  <packaging>pom</packaging>
  <properties>
    <firebase-analytics.version>22.0.2</firebase-analytics.version>
    <firebase-auth.version>23.0.0</firebase-auth.version>
    <firebase-firestore.version>25.0.0</firebase-firestore.version>
  </properties>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>com.google.firebase</groupId>
        <artifactId>firebase-analytics</artifactId>
        <version>${firebase-analytics.version}</version>
      </dependency>
      <dependency>
        <groupId>com.google.firebase</groupId>
        <artifactId>firebase-auth</artifactId>
        <version>${firebase-auth.version}</version>
      </dependency>
      <dependency>
        <groupId>com.google.firebase</groupId>
        <artifactId>firebase-firestore</artifactId>
        <version>${firebase-firestore.version}</version>
      </dependency>
    </dependencies>
  </dependencyManagement>
</project>`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write pom: %v", err)
	}

	got, err := parseBOM(path)
	if err != nil {
		t.Fatalf("parseBOM: %v", err)
	}

	expected := map[string]string{
		"com.google.firebase:firebase-analytics": "22.0.2",
		"com.google.firebase:firebase-auth":      "23.0.0",
		"com.google.firebase:firebase-firestore": "25.0.0",
	}
	if len(got) != len(expected) {
		t.Fatalf("expected %d managed versions, got %d: %v", len(expected), len(got), got)
	}
	for key, want := range expected {
		if got[key] != want {
			t.Fatalf("managed version for %s: got %q, want %q", key, got[key], want)
		}
	}
}

func TestLoadBOMFromCacheLayout(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()
	resolver := New(cacheRoot, t.TempDir(), nil, nil)

	// Set up cache directory structure: group/module/version/hash/artifact.pom
	coord := Coordinate{Group: "androidx.compose", Module: "compose-bom", Version: "2024.06.00"}
	base := resolver.moduleBasePath(coord)
	hashDir := filepath.Join(base, "abc123")
	if err := os.MkdirAll(hashDir, 0o755); err != nil {
		t.Fatal(err)
	}

	pom := `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>androidx.compose</groupId>
  <artifactId>compose-bom</artifactId>
  <version>2024.06.00</version>
  <packaging>pom</packaging>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>androidx.compose.ui</groupId>
        <artifactId>ui</artifactId>
        <version>1.6.8</version>
      </dependency>
      <dependency>
        <groupId>androidx.compose.material3</groupId>
        <artifactId>material3</artifactId>
        <version>1.2.1</version>
      </dependency>
    </dependencies>
  </dependencyManagement>
</project>`
	if err := os.WriteFile(filepath.Join(hashDir, "compose-bom-2024.06.00.pom"), []byte(pom), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := resolver.loadBOM(coord)
	if err != nil {
		t.Fatalf("loadBOM: %v", err)
	}

	if got["androidx.compose.ui:ui"] != "1.6.8" {
		t.Fatalf("expected ui version 1.6.8, got %q", got["androidx.compose.ui:ui"])
	}
	if got["androidx.compose.material3:material3"] != "1.2.1" {
		t.Fatalf("expected material3 version 1.2.1, got %q", got["androidx.compose.material3:material3"])
	}
}

func TestParseBOMIgnoresDepsWithoutVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "partial-bom.pom")
	body := `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>example-bom</artifactId>
  <version>1.0.0</version>
  <packaging>pom</packaging>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>com.example</groupId>
        <artifactId>lib-a</artifactId>
        <version>2.0.0</version>
      </dependency>
      <dependency>
        <groupId>com.example</groupId>
        <artifactId>lib-b</artifactId>
      </dependency>
    </dependencies>
  </dependencyManagement>
</project>`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write pom: %v", err)
	}

	got, err := parseBOM(path)
	if err != nil {
		t.Fatalf("parseBOM: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 managed version (deps without version should be skipped), got %d: %v", len(got), got)
	}
	if got["com.example:lib-a"] != "2.0.0" {
		t.Fatalf("expected lib-a version 2.0.0, got %q", got["com.example:lib-a"])
	}
}

func TestParsePOMDepsResolvesProjectVersionPropertyWithParentVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact.pom")
	body := `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <modelVersion>4.0.0</modelVersion>
  <parent>
    <groupId>org.sonatype.oss</groupId>
    <artifactId>oss-parent</artifactId>
    <version>7</version>
  </parent>
  <groupId>com.google.dagger</groupId>
  <artifactId>hilt-android</artifactId>
  <version>2.57.2</version>
  <dependencies>
    <dependency>
      <groupId>com.google.dagger</groupId>
      <artifactId>hilt-core</artifactId>
      <version>${project.version}</version>
    </dependency>
  </dependencies>
</project>`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write pom: %v", err)
	}
	got, err := parsePOMDeps(path)
	if err != nil {
		t.Fatalf("parsePOMDeps: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 dependency, got %#v", got)
	}
	if got[0].Version != "2.57.2" {
		t.Fatalf("expected resolved version 2.57.2, got %#v", got[0])
	}
}
