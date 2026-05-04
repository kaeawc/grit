package dependencywiring

import (
	"errors"
	"testing"

	"github.com/kaeawc/grit/internal/project"
)

func TestResolverWithLegacyPathSucceeds(t *testing.T) {
	prj := &project.Project{RootDir: t.TempDir()}
	r, err := ResolverWith(prj, nil, ResolverOptions{UseTieredCache: false})
	if err != nil {
		t.Fatalf("ResolverWith legacy: %v", err)
	}
	if r == nil {
		t.Fatal("expected non-nil resolver")
	}
}

func TestResolverWithTieredCacheReturnsSentinel(t *testing.T) {
	prj := &project.Project{RootDir: t.TempDir()}
	_, err := ResolverWith(prj, nil, ResolverOptions{UseTieredCache: true})
	if !errors.Is(err, ErrTieredCacheUnavailable) {
		t.Fatalf("expected ErrTieredCacheUnavailable, got %v", err)
	}
}

func TestResolverHonoursEnvFlag(t *testing.T) {
	t.Setenv(EnvUseTieredCache, "1")
	prj := &project.Project{RootDir: t.TempDir()}
	_, err := Resolver(prj, nil)
	if !errors.Is(err, ErrTieredCacheUnavailable) {
		t.Fatalf("expected ErrTieredCacheUnavailable when env flag set, got %v", err)
	}
}

func TestResolverFallsBackWithoutEnvFlag(t *testing.T) {
	t.Setenv(EnvUseTieredCache, "")
	prj := &project.Project{RootDir: t.TempDir()}
	r, err := Resolver(prj, nil)
	if err != nil {
		t.Fatalf("Resolver default: %v", err)
	}
	if r == nil {
		t.Fatal("expected legacy resolver when env flag is unset")
	}
}

func TestResolverWithRejectsNilProject(t *testing.T) {
	_, err := ResolverWith(nil, nil, ResolverOptions{})
	if err == nil {
		t.Fatal("expected error for nil project")
	}
}
