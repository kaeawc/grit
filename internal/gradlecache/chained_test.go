package gradlecache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/project"
)

func TestLocalFindJarsFindsShallowAndNestedLayouts(t *testing.T) {
	root := t.TempDir()
	// Shallow layout: <root>/<g>/<m>/<v>/<file>
	shallowDir := filepath.Join(root, "com.example", "lib", "1.0")
	if err := os.MkdirAll(shallowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shallowJar := filepath.Join(shallowDir, "lib-1.0.jar")
	if err := os.WriteFile(shallowJar, []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Nested layout (gradle's modules-2 shape): <root>/<g>/<m>/<v>/<hash>/<file>
	nestedJar := seedJar(t, root, "com.example", "other", "2.0", "h", "other-2.0.jar")

	probe := NewProbe(root)
	if got := probe.FindJars("com.example", "lib", "1.0"); len(got) != 1 || got[0] != shallowJar {
		t.Fatalf("shallow layout: got %#v want [%s]", got, shallowJar)
	}
	if got := probe.FindJars("com.example", "other", "2.0"); len(got) != 1 || got[0] != nestedJar {
		t.Fatalf("nested layout: got %#v want [%s]", got, nestedJar)
	}
}

func TestStagedJarIsFoundOnRepeatLookupWithoutFallbackDescent(t *testing.T) {
	primaryRoot := t.TempDir()
	fallbackRoot := t.TempDir()
	seedJar(t, fallbackRoot, "com.example", "lib", "1.0", "h", "lib-1.0.jar")

	probe := NewProbe(primaryRoot).WithFallback(NewProbe(fallbackRoot)).WithStaging(StageByHardlink)
	first := probe.FindJars("com.example", "lib", "1.0")
	if len(first) != 1 {
		t.Fatalf("first call expected one jar, got %#v", first)
	}

	// A primary-only probe must find the staged file on the next lookup;
	// otherwise the chain re-stages on every call, defeating staging.
	localOnly := NewProbe(primaryRoot).FindJars("com.example", "lib", "1.0")
	if len(localOnly) != 1 || localOnly[0] != first[0] {
		t.Fatalf("staged jar not re-found locally; got %#v, want [%s]", localOnly, first[0])
	}
}

func TestProbeFetcherRunsOnExhaustedChain(t *testing.T) {
	primaryRoot := t.TempDir()
	fallbackRoot := t.TempDir()

	calls := 0
	fetcher := FetcherFunc(func(destDir, group, module, version string) ([]string, error) {
		calls++
		dest := filepath.Join(destDir, module+"-"+version+".jar")
		if err := os.WriteFile(dest, []byte("fetched"), 0o644); err != nil {
			return nil, err
		}
		return []string{dest}, nil
	})
	probe := NewProbe(primaryRoot).WithFallback(NewProbe(fallbackRoot)).WithFetcher(fetcher)
	got := probe.FindJars("com.example", "lib", "1.0")
	if len(got) != 1 || filepath.Base(got[0]) != "lib-1.0.jar" {
		t.Fatalf("expected fetched jar, got %#v", got)
	}
	if calls != 1 {
		t.Fatalf("expected fetcher invoked once, got %d", calls)
	}
	// Second call must hit the local layout without re-fetching.
	if got2 := probe.FindJars("com.example", "lib", "1.0"); len(got2) != 1 || got2[0] != got[0] {
		t.Fatalf("expected stable local hit on second call, got %#v", got2)
	}
	if calls != 1 {
		t.Fatalf("expected no re-fetch on local hit, got %d total calls", calls)
	}
}

func TestProbeFetcherSkippedWhenFallbackHits(t *testing.T) {
	primaryRoot := t.TempDir()
	fallbackRoot := t.TempDir()
	wantJar := seedJar(t, fallbackRoot, "com.example", "lib", "1.0", "h", "lib-1.0.jar")

	calls := 0
	fetcher := FetcherFunc(func(string, string, string, string) ([]string, error) {
		calls++
		return nil, nil
	})
	probe := NewProbe(primaryRoot).WithFallback(NewProbe(fallbackRoot)).WithFetcher(fetcher)
	got := probe.FindJars("com.example", "lib", "1.0")
	if len(got) != 1 || got[0] != wantJar {
		t.Fatalf("expected fallback hit, got %#v", got)
	}
	if calls != 0 {
		t.Fatalf("expected no fetcher invocation, got %d", calls)
	}
}

func TestProbeFetcherFailureLeavesNoHit(t *testing.T) {
	primaryRoot := t.TempDir()
	fetcher := FetcherFunc(func(string, string, string, string) ([]string, error) {
		return nil, errors.New("network down")
	})
	probe := NewProbe(primaryRoot).WithFetcher(fetcher)
	if got := probe.FindJars("com.example", "lib", "1.0"); len(got) != 0 {
		t.Fatalf("expected empty result on fetcher error, got %#v", got)
	}
}

func TestProbeFallbackResolvesOnPrimaryMiss(t *testing.T) {
	primaryRoot := t.TempDir()
	fallbackRoot := t.TempDir()
	wantJar := seedJar(t, fallbackRoot, "com.example", "lib", "1.0", "h", "lib-1.0.jar")

	probe := NewProbe(primaryRoot).WithFallback(NewProbe(fallbackRoot))
	got := probe.FindJars("com.example", "lib", "1.0")
	if len(got) != 1 || got[0] != wantJar {
		t.Fatalf("expected fallback hit, got %#v", got)
	}
}

func TestProbeFallbackPrefersPrimaryWhenPresent(t *testing.T) {
	primaryRoot := t.TempDir()
	fallbackRoot := t.TempDir()
	primaryJar := seedJar(t, primaryRoot, "com.example", "lib", "1.0", "h", "lib-1.0.jar")
	seedJar(t, fallbackRoot, "com.example", "lib", "1.0", "h", "lib-1.0.jar")

	probe := NewProbe(primaryRoot).WithFallback(NewProbe(fallbackRoot))
	got := probe.FindJars("com.example", "lib", "1.0")
	if len(got) != 1 || got[0] != primaryJar {
		t.Fatalf("expected primary hit (no fallback descent), got %#v", got)
	}
}

func TestProbeFallbackVersionsUnion(t *testing.T) {
	primaryRoot := t.TempDir()
	fallbackRoot := t.TempDir()
	seedJar(t, primaryRoot, "com.example", "lib", "1.1", "h", "lib-1.1.jar")
	seedJar(t, fallbackRoot, "com.example", "lib", "1.0", "h", "lib-1.0.jar")
	seedJar(t, fallbackRoot, "com.example", "lib", "1.1", "h", "lib-1.1.jar")

	probe := NewProbe(primaryRoot).WithFallback(NewProbe(fallbackRoot))
	got := probe.Versions("com.example", "lib", nil)
	if len(got) != 2 || got[0] != "1.0" || got[1] != "1.1" {
		t.Fatalf("expected deduplicated union sorted lexicographically, got %#v", got)
	}
}

func TestProbeWithStagingHardlinksFromFallback(t *testing.T) {
	primaryRoot := t.TempDir()
	fallbackRoot := t.TempDir()
	fallbackJar := seedJar(t, fallbackRoot, "com.example", "lib", "1.0", "h", "lib-1.0.jar")

	probe := NewProbe(primaryRoot).WithFallback(NewProbe(fallbackRoot)).WithStaging(StageByHardlink)
	got := probe.FindJars("com.example", "lib", "1.0")
	if len(got) != 1 {
		t.Fatalf("expected one jar after staging, got %#v", got)
	}
	if !strings.HasPrefix(got[0], primaryRoot) {
		t.Fatalf("expected staged path under primary root, got %q (primary=%q)", got[0], primaryRoot)
	}
	if _, err := os.Stat(got[0]); err != nil {
		t.Fatalf("staged file should exist: %v", err)
	}
	// A second call should still resolve via the primary root.
	got2 := probe.FindJars("com.example", "lib", "1.0")
	if len(got2) != 1 || got2[0] != got[0] {
		t.Fatalf("expected stable staged path on second call, got %#v", got2)
	}
	// Fallback file must remain untouched (hardlink, not move).
	if _, err := os.Stat(fallbackJar); err != nil {
		t.Fatalf("fallback file should remain: %v", err)
	}
}

func TestProbeWithStagingFallsBackToCopyOnLinkFailure(t *testing.T) {
	primaryRoot := t.TempDir()
	fallbackRoot := t.TempDir()
	wantSource := seedJar(t, fallbackRoot, "com.example", "lib", "1.0", "h", "lib-1.0.jar")

	stagerCalls := 0
	probe := NewProbe(primaryRoot).WithFallback(NewProbe(fallbackRoot)).WithStaging(StagerFunc(func(destDir, sourcePath string) (string, error) {
		stagerCalls++
		if sourcePath != wantSource {
			return "", errors.New("stager: unexpected source")
		}
		// Simulate failure -> probe should keep returning the source path.
		return "", errors.New("simulated link failure")
	}))

	got := probe.FindJars("com.example", "lib", "1.0")
	if len(got) != 1 || got[0] != wantSource {
		t.Fatalf("expected source path passthrough on stager failure, got %#v", got)
	}
	if stagerCalls != 1 {
		t.Fatalf("expected stager to be invoked exactly once, got %d", stagerCalls)
	}
}

func TestProjectProbeFallsThroughToDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	gradleRoot := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
	wantJar := seedJar(t, gradleRoot, "com.example", "lib", "1.0", "h", "lib-1.0.jar")

	probe := ProjectProbe(&project.Project{RootDir: t.TempDir()})
	got := probe.FindJars("com.example", "lib", "1.0")
	if len(got) != 1 {
		t.Fatalf("expected one jar via fallback chain, got %#v", got)
	}
	if !strings.Contains(got[0], ".grit/cache/artifacts") {
		t.Fatalf("expected the hit to be staged into the project's grit cache, got %q (original=%q)", got[0], wantJar)
	}
}

func TestProjectProbeStagesIntoProjectTree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	gradleRoot := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
	seedJar(t, gradleRoot, "com.example", "lib", "1.0", "h", "lib-1.0.jar")

	projectRoot := t.TempDir()
	probe := ProjectProbe(&project.Project{RootDir: projectRoot})
	got := probe.FindJars("com.example", "lib", "1.0")
	if len(got) != 1 {
		t.Fatalf("expected one jar, got %#v", got)
	}
	expectedPrefix := filepath.Join(projectRoot, ".grit", "cache", "artifacts", "com.example", "lib", "1.0")
	if !strings.HasPrefix(got[0], expectedPrefix) {
		t.Fatalf("expected staged path under %q, got %q", expectedPrefix, got[0])
	}
}

func TestProjectProbeFallsBackToDefaultWhenProjectMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	gradleRoot := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
	wantJar := seedJar(t, gradleRoot, "com.example", "lib", "1.0", "h", "lib-1.0.jar")

	for _, prj := range []*project.Project{nil, {RootDir: ""}} {
		got := ProjectProbe(prj).FindJars("com.example", "lib", "1.0")
		if len(got) != 1 || got[0] != wantJar {
			t.Fatalf("expected DefaultProbe fallback for %#v, got %#v", prj, got)
		}
	}
}

func TestProjectStagingRoot(t *testing.T) {
	if got := ProjectStagingRoot(nil); got != "" {
		t.Fatalf("nil project: got %q", got)
	}
	if got := ProjectStagingRoot(&project.Project{}); got != "" {
		t.Fatalf("empty root: got %q", got)
	}
	if got := ProjectStagingRoot(&project.Project{RootDir: "/repo"}); got != "/repo/.grit/cache/artifacts" {
		t.Fatalf("unexpected staging root: %q", got)
	}
}
