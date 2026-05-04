package dependencywiring

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/m2local"
)

func TestResolvedInputDigestIsDeterministic(t *testing.T) {
	t.Parallel()

	resolved := &m2local.Resolved{
		CompileJars: []string{"/a/b.jar", "/c/d.jar"},
		RuntimeJars: []string{"/e/f.jar"},
	}
	d1 := resolvedInputDigest(resolved)
	d2 := resolvedInputDigest(resolved)
	if d1 != d2 {
		t.Fatalf("digest not deterministic: %s vs %s", d1, d2)
	}
	if d1 == "" {
		t.Fatal("expected non-empty digest")
	}
}

func TestResolvedInputDigestChangesWithPaths(t *testing.T) {
	t.Parallel()

	r1 := &m2local.Resolved{CompileJars: []string{"/a.jar"}}
	r2 := &m2local.Resolved{CompileJars: []string{"/b.jar"}}
	if resolvedInputDigest(r1) == resolvedInputDigest(r2) {
		t.Fatal("expected different digests for different paths")
	}
}

func TestResolvedInputDigestIncludesAndroidLibraries(t *testing.T) {
	t.Parallel()

	base := &m2local.Resolved{CompileJars: []string{"/a.jar"}}
	withLib := &m2local.Resolved{
		CompileJars:      []string{"/a.jar"},
		AndroidLibraries: []m2local.AndroidLibrary{{ID: "maven:com.example:lib:1.0"}},
	}
	if resolvedInputDigest(base) == resolvedInputDigest(withLib) {
		t.Fatal("expected different digests when android libraries differ")
	}
}

func TestResolvedInputDigestNilIsEmpty(t *testing.T) {
	t.Parallel()
	if resolvedInputDigest(nil) != "" {
		t.Fatal("expected empty digest for nil")
	}
}

func TestSaveLockfileAndLoadCachedLockfile(t *testing.T) {
	root := t.TempDir()

	resolved := &m2local.Resolved{
		CompileJars: []string{"/gradle/org/example/demo/1.0/abc/demo.jar"},
		RuntimeJars: []string{"/gradle/org/example/demo/1.0/abc/demo.jar"},
	}

	lf := lockfile.Lockfile{
		SchemaVersion: lockfile.CurrentSchemaVersion,
		GeneratedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		GritVersion:   "test",
		Pins: []lockfile.Pin{
			{
				Coordinate:   lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.0"},
				RepositoryID: "gradle-cache",
				Files: []lockfile.PinFile{
					{Kind: lockfile.FileKindPrimary, Name: "demo.jar", Size: 100, Hash: cas.Hash{1, 2, 3}},
				},
			},
		},
	}

	if err := saveLockfile(root, lf, resolved); err != nil {
		t.Fatalf("saveLockfile: %v", err)
	}

	// Verify files were created.
	if _, err := os.Stat(lockfilePath(root)); err != nil {
		t.Fatalf("lockfile not created: %v", err)
	}
	if _, err := os.Stat(lockfileMetaPath(root)); err != nil {
		t.Fatalf("meta not created: %v", err)
	}

	// Load with matching resolved output should hit.
	cached, ok := loadCachedLockfile(root, resolved)
	if !ok {
		t.Fatal("expected cache hit with matching resolved")
	}
	if len(cached.Pins) != 1 {
		t.Fatalf("expected 1 pin, got %d", len(cached.Pins))
	}
	if cached.Pins[0].Coordinate.Group != "org.example" {
		t.Fatalf("unexpected coordinate: %#v", cached.Pins[0].Coordinate)
	}

	// Load with different resolved output should miss.
	different := &m2local.Resolved{
		CompileJars: []string{"/gradle/org/example/other/2.0/xyz/other.jar"},
	}
	if _, ok := loadCachedLockfile(root, different); ok {
		t.Fatal("expected cache miss with different resolved")
	}
}

func TestLoadCachedLockfileMissesWhenNoFiles(t *testing.T) {
	root := t.TempDir()
	resolved := &m2local.Resolved{CompileJars: []string{"/a.jar"}}
	if _, ok := loadCachedLockfile(root, resolved); ok {
		t.Fatal("expected miss when no lockfile persisted")
	}
}

func TestSaveLockfileCreatesDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "deep")
	resolved := &m2local.Resolved{CompileJars: []string{"/a.jar"}}
	lf := lockfile.Lockfile{
		SchemaVersion: lockfile.CurrentSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Pins: []lockfile.Pin{
			{
				Coordinate:   lockfile.Coordinate{Group: "g", Artifact: "a", Version: "1"},
				RepositoryID: "test",
				Files:        []lockfile.PinFile{{Kind: lockfile.FileKindPrimary, Name: "a.jar", Size: 1, Hash: cas.Hash{1}}},
			},
		},
	}
	if err := saveLockfile(root, lf, resolved); err != nil {
		t.Fatalf("saveLockfile should create parent dirs: %v", err)
	}
	if _, err := os.Stat(lockfilePath(root)); err != nil {
		t.Fatalf("lockfile not found after save: %v", err)
	}
}

func TestLockfilePathExported(t *testing.T) {
	t.Parallel()
	if LockfilePath("") != "" {
		t.Fatal("expected empty for empty workRoot")
	}
	got := LockfilePath("/repo")
	want := filepath.Join("/repo", ".grit", "worktree", "dependencies.lock.json")
	if got != want {
		t.Fatalf("LockfilePath: got %q want %q", got, want)
	}
}

func TestLockfileCacheFastPathInMaterializer(t *testing.T) {
	// This test verifies that lockfilePins uses the cached lockfile
	// when the resolved input digest matches, by checking that no
	// produce step is needed (the resolved has no actual Gradle
	// cache paths, so produce would fail without the cache).
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()

	resolved := &m2local.Resolved{
		CompileJars: []string{"/nonexistent/path/demo.jar"},
	}

	lf := lockfile.Lockfile{
		SchemaVersion: lockfile.CurrentSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		GritVersion:   "test",
		Pins: []lockfile.Pin{
			{
				Coordinate:   lockfile.Coordinate{Group: "org.test", Artifact: "cached", Version: "1.0"},
				RepositoryID: "test-repo",
				Files: []lockfile.PinFile{
					{Kind: lockfile.FileKindPrimary, Name: "cached.jar", Size: 50, Hash: cas.Hash{9, 8, 7}},
				},
			},
		},
	}

	if err := saveLockfile(root, lf, resolved); err != nil {
		t.Fatalf("seed lockfile: %v", err)
	}

	mat := &stackMaterializer{
		workRoot:  root,
		cacheRoot: filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1"),
	}

	pins, err := mat.lockfilePins(resolved)
	if err != nil {
		t.Fatalf("lockfilePins: %v", err)
	}
	if len(pins) != 1 {
		t.Fatalf("expected 1 cached pin, got %d", len(pins))
	}
	if pins[0].Coordinate.Artifact != "cached" {
		t.Fatalf("expected cached pin, got %#v", pins[0])
	}
}
