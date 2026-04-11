package m2local

import (
	"os"
	"path/filepath"
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
