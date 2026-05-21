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

// seedJar materializes an empty jar at the layout the probe expects so
// tests can assert path discovery without relying on byte content.
func seedJar(t *testing.T, root, group, module, version, hash, filename string) string {
	t.Helper()
	dir := filepath.Join(root, group, module, version, hash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProbeFindJarsRespectsCustomRoot(t *testing.T) {
	root := t.TempDir()
	probe := NewProbe(root)

	wantJar := seedJar(t, root, "com.example", "lib", "1.0", "h", "lib-1.0.jar")
	seedJar(t, root, "com.example", "lib", "1.0", "h", "lib-1.0-sources.jar")

	got := probe.FindJars("com.example", "lib", "1.0")
	if len(got) != 1 || got[0] != wantJar {
		t.Fatalf("expected one jar (no sources/javadoc), got %#v", got)
	}
	if first := probe.FirstJar("com.example", "lib", "1.0"); first != wantJar {
		t.Fatalf("unexpected first jar: %q", first)
	}
}

func TestProbeFindJarsTreatsEmptyRootAsAbsent(t *testing.T) {
	if jars := NewProbe("").FindJars("g", "m", "1"); jars != nil {
		t.Fatalf("expected nil for empty-rooted probe, got %#v", jars)
	}
	var nilProbe *Probe
	if jars := nilProbe.FindJars("g", "m", "1"); jars != nil {
		t.Fatalf("expected nil for nil probe, got %#v", jars)
	}
}

func TestProbeVersionsAndLatest(t *testing.T) {
	root := t.TempDir()
	for _, v := range []string{"1.0", "1.1", "2.0"} {
		seedJar(t, root, "com.example", "lib", v, "h", "lib-"+v+".jar")
	}
	probe := NewProbe(root)

	versions := probe.Versions("com.example", "lib", nil)
	if len(versions) != 3 || versions[0] != "1.0" || versions[2] != "2.0" {
		t.Fatalf("unexpected versions: %#v", versions)
	}
	if got := probe.LatestVersion("com.example", "lib"); got != "2.0" {
		t.Fatalf("unexpected latest: %q", got)
	}
}

func TestProbeDependenciesReadsModuleMetadata(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "com.example", "lib", "1.0", "hash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := `{"variants":[{"attributes":{"org.gradle.usage":"java-runtime"},"dependencies":[{"group":"com.example","module":"core","version":{"requires":"1.0"}}]}]}`
	if err := os.WriteFile(filepath.Join(dir, "lib-1.0.module"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := NewProbe(root).Dependencies("com.example", "lib", "1.0")
	if len(deps) != 1 || deps[0].Module != "core" {
		t.Fatalf("unexpected deps: %#v", deps)
	}
}

func TestProbeRootAccessor(t *testing.T) {
	if got := NewProbe("/tmp/example").Root(); got != "/tmp/example" {
		t.Fatalf("Root: got %q", got)
	}
	var nilProbe *Probe
	if got := nilProbe.Root(); got != "" {
		t.Fatalf("nil Root: got %q", got)
	}
	if got := NewProbe("").Root(); got != "" {
		t.Fatalf("empty Root: got %q", got)
	}
}

func TestProbeVersionsHonorsCustomCompare(t *testing.T) {
	root := t.TempDir()
	for _, v := range []string{"1.0", "10.0", "2.0"} {
		seedJar(t, root, "com.example", "lib", v, "h", "lib-"+v+".jar")
	}
	probe := NewProbe(root)

	descending := func(a, b string) int {
		switch {
		case a > b:
			return -1
		case a < b:
			return 1
		default:
			return 0
		}
	}
	got := probe.Versions("com.example", "lib", descending)
	if len(got) != 3 || got[0] != "2.0" || got[2] != "1.0" {
		t.Fatalf("expected custom compare to sort descending lexicographically, got %#v", got)
	}
}

func TestProbeDependenciesFallsBackToPOM(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "com.example", "lib", "1.0", "hash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pom := `<project><dependencies><dependency><groupId>com.example</groupId><artifactId>core</artifactId><version>1.0</version></dependency></dependencies></project>`
	if err := os.WriteFile(filepath.Join(dir, "lib-1.0.pom"), []byte(pom), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := NewProbe(root).Dependencies("com.example", "lib", "1.0")
	if len(deps) != 1 || deps[0].Module != "core" {
		t.Fatalf("expected POM fallback via probe, got %#v", deps)
	}
}

func TestNilProbeMethodsReturnZeroValues(t *testing.T) {
	var p *Probe
	if got := p.Versions("g", "m", nil); got != nil {
		t.Fatalf("nil Versions: got %#v", got)
	}
	if got := p.LatestVersion("g", "m"); got != "" {
		t.Fatalf("nil LatestVersion: got %q", got)
	}
	if got := p.Dependencies("g", "m", "1"); got != nil {
		t.Fatalf("nil Dependencies: got %#v", got)
	}
	if got := p.FirstJar("g", "m", "1"); got != "" {
		t.Fatalf("nil FirstJar: got %q", got)
	}
}

func TestPackageGlobalsDelegateToDefaultProbe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wantJar := seedJar(t, filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1"), "com.example", "lib", "1.0", "h", "lib-1.0.jar")

	if got := FirstArtifactJar("com.example", "lib", "1.0"); got != wantJar {
		t.Fatalf("package global should resolve via default probe: got %q want %q", got, wantJar)
	}
	if got := LatestVersion("com.example", "lib"); got != "1.0" {
		t.Fatalf("package global LatestVersion: got %q", got)
	}
}
