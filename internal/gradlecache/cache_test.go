package gradlecache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactDependenciesReadsGradleModuleMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1", "org.junit.platform", "junit-platform-launcher", "1.10.2", "hash")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := `{"variants":[{"attributes":{"org.gradle.usage":"java-runtime"},"dependencies":[{"group":"org.junit.platform","module":"junit-platform-engine","version":{"requires":"1.10.2"}},{"group":"org.apiguardian","module":"apiguardian-api","version":{"requires":"1.1.2"}}]}]}`
	if err := os.WriteFile(filepath.Join(root, "junit-platform-launcher-1.10.2.module"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ArtifactDependencies("org.junit.platform", "junit-platform-launcher", "1.10.2")
	if len(got) != 2 {
		t.Fatalf("expected two deps, got %#v", got)
	}
	if got[0].Group != "org.junit.platform" || got[0].Module != "junit-platform-engine" || got[0].Version != "1.10.2" {
		t.Fatalf("unexpected first dep: %#v", got[0])
	}
}

func TestArtifactDependenciesFallsBackToPOM(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1", "org.junit.jupiter", "junit-jupiter-engine", "5.10.2", "hash")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := `<project><dependencies><dependency><groupId>org.junit.jupiter</groupId><artifactId>junit-jupiter-api</artifactId><version>5.10.2</version></dependency><dependency><groupId>x</groupId><artifactId>y</artifactId><version>1</version><scope>test</scope></dependency></dependencies></project>`
	if err := os.WriteFile(filepath.Join(root, "junit-jupiter-engine-5.10.2.pom"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ArtifactDependencies("org.junit.jupiter", "junit-jupiter-engine", "5.10.2")
	if len(got) != 1 || got[0].Group != "org.junit.jupiter" || got[0].Module != "junit-jupiter-api" || got[0].Version != "5.10.2" {
		t.Fatalf("unexpected deps: %#v", got)
	}
}
