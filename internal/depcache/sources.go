package depcache

import (
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/kaeawc/grit/internal/downloader"
	"github.com/kaeawc/grit/internal/downloader/gradlecache"
	mavenread "github.com/kaeawc/grit/internal/downloader/mavenlocal"
	"github.com/kaeawc/grit/internal/downloader/mavenremote"
	"github.com/kaeawc/grit/internal/downloader/retry"
	"github.com/kaeawc/grit/internal/project"
)

// SourceDownloaders builds the ordered downloader chain for a project's
// configured repositories: an optional gradle-cache fast path first,
// then declared repositories in declaration order, with an implicit
// maven-local fallback appended when the project does not declare one
// itself. Duplicate downloaders (same ID) are removed, keeping the
// first occurrence.
func SourceDownloaders(repos []project.Repository, gradleCacheRoot string) []downloader.Downloader {
	var sources []downloader.Downloader
	if gradleCacheRoot != "" {
		sources = append(sources, gradlecache.New(gradleCacheRoot))
	}

	hasDeclaredMavenLocal := false
	for _, repo := range repos {
		if repo.Kind == "mavenLocal" {
			hasDeclaredMavenLocal = true
		}
		if dl := downloaderForRepository(repo); dl != nil {
			sources = append(sources, dl)
		}
	}

	if !hasDeclaredMavenLocal {
		if root := mavenread.DefaultRoot(); root != "" {
			sources = append(sources, mavenread.New(root))
		}
	}

	return DeduplicateDownloaders(sources)
}

// DeduplicateDownloaders removes duplicate downloaders by ID, keeping
// the first occurrence. This prevents the same source (e.g. maven-local
// at the default root) from appearing twice when it is both implicitly
// added and explicitly declared.
func DeduplicateDownloaders(sources []downloader.Downloader) []downloader.Downloader {
	seen := map[string]bool{}
	out := make([]downloader.Downloader, 0, len(sources))
	for _, dl := range sources {
		id := dl.ID()
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, dl)
	}
	return out
}

func downloaderForRepository(repo project.Repository) downloader.Downloader {
	switch repo.Kind {
	case "mavenLocal":
		if root := fileURLPath(repo.URL); root != "" {
			return mavenread.New(root)
		}
		if root := mavenread.DefaultRoot(); root != "" {
			return mavenread.New(root)
		}
		return nil
	case "maven", "google", "mavenCentral", "gradlePluginPortal", "jcenter":
		if root := fileURLPath(repo.URL); root != "" {
			return mavenread.New(root)
		}
		if strings.TrimSpace(repo.URL) == "" {
			return nil
		}
		id := repo.Name
		if id == "" {
			id = repo.Kind
		}
		remote, err := mavenremote.New(repo.URL, mavenremote.WithID(id))
		if err != nil {
			return nil
		}
		wrapped, err := retry.New(remote, retry.WithAttempts(3), retry.WithBackoff(func(attempt int) time.Duration {
			return time.Duration(attempt) * 10 * time.Millisecond
		}))
		if err != nil {
			return remote
		}
		return wrapped
	default:
		return nil
	}
}

func fileURLPath(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return filepath.FromSlash(u.Path)
}
