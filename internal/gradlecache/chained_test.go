package gradlecache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/project"
)

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
