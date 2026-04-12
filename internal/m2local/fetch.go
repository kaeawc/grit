package m2local

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kaeawc/grit/internal/griterr"
	"github.com/kaeawc/grit/internal/pathutil"
	"github.com/kaeawc/grit/internal/project"
)

func (r *Resolver) fetchPOM(coord Coordinate) (string, error) {
	relPath := strings.ReplaceAll(coord.Group, ".", "/") + "/" + coord.Module + "/" + coord.Version + "/" + coord.Module + "-" + coord.Version + ".pom"
	outPath := filepath.Join(r.WorkRoot, ".grit", "metadata", coord.Group, coord.Module, coord.Version, coord.Module+"-"+coord.Version+".pom")
	return r.fetchRemoteFile(coord, relPath, outPath, "pom")
}

func (r *Resolver) fetchModuleMetadata(coord Coordinate) (string, error) {
	relPath := strings.ReplaceAll(coord.Group, ".", "/") + "/" + coord.Module + "/" + coord.Version + "/" + coord.Module + "-" + coord.Version + ".module"
	outPath := filepath.Join(r.WorkRoot, ".grit", "metadata", coord.Group, coord.Module, coord.Version, coord.Module+"-"+coord.Version+".module")
	return r.fetchRemoteFile(coord, relPath, outPath, "module metadata")
}

func (r *Resolver) fetchArtifact(coord Coordinate, ext string) (string, error) {
	if ext != ".jar" && ext != ".aar" {
		return "", griterr.Newf(griterr.ErrUnsupported, "artifact extension %q", ext)
	}
	relPath := strings.ReplaceAll(coord.Group, ".", "/") + "/" + coord.Module + "/" + coord.Version + "/" + coord.Module + "-" + coord.Version + ext
	outPath := filepath.Join(r.moduleBasePath(coord), "downloaded", coord.Module+"-"+coord.Version+ext)
	return r.fetchRemoteFile(coord, relPath, outPath, ext[1:])
}

func (r *Resolver) fetchRemoteFile(coord Coordinate, relPath, outPath, label string) (string, error) {
	if fileExists(outPath) {
		r.recordFetchedSource(outPath, ResolutionMetadataSource{
			Kind:    metadataKindForLabel(label),
			Path:    outPath,
			Fetched: true,
		})
		return outPath, nil
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", err
	}
	for _, baseURL := range r.remoteRepositoryURLs(coord) {
		resp, err := http.Get(baseURL + relPath)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", err
		}
		tmpPath := outPath + ".tmp"
		if err := os.WriteFile(tmpPath, body, 0o644); err != nil {
			return "", err
		}
		if err := os.Rename(tmpPath, outPath); err != nil {
			if !fileExists(outPath) {
				return "", err
			}
			_ = os.Remove(tmpPath)
		}
		r.recordFetchedSource(outPath, ResolutionMetadataSource{
			Kind:          metadataKindForLabel(label),
			Path:          outPath,
			RepositoryURL: baseURL,
			Fetched:       true,
		})
		return outPath, nil
	}
	return "", fmt.Errorf("%s not found for %s", label, coordinateID(coord))
}

func metadataKindForLabel(label string) string {
	switch label {
	case "module metadata":
		return "module"
	case "pom":
		return "pom"
	default:
		return label
	}
}

func (r *Resolver) recordFetchedSource(path string, source ResolutionMetadataSource) {
	if path == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fetched == nil {
		r.fetched = map[string]ResolutionMetadataSource{}
	}
	r.fetched[path] = source
}

func (r *Resolver) metadataSourceForPath(path, kind string) *ResolutionMetadataSource {
	if path == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if source, ok := r.fetched[path]; ok {
		copy := source
		if copy.Kind == "" {
			copy.Kind = kind
		}
		if copy.Path == "" {
			copy.Path = path
		}
		return &copy
	}
	return &ResolutionMetadataSource{
		Kind: kind,
		Path: path,
	}
}

func (r *Resolver) remoteRepositoryURLs(coord Coordinate) []string {
	var urls []string
	for _, repo := range r.Repositories {
		if repo.Scope != "dependency" && repo.Scope != "" {
			continue
		}
		if repo.Kind == "mavenLocal" || repo.URL == "" {
			continue
		}
		if !repoAllowsCoordinate(repo, coord) {
			continue
		}
		urls = append(urls, pathutil.EnsureTrailingSlash(repo.URL))
	}
	if len(urls) == 0 {
		urls = append(urls, "https://dl.google.com/dl/android/maven2/", "https://repo1.maven.org/maven2/")
	}
	return mergeUnique(nil, urls)
}

func repoAllowsCoordinate(repo project.Repository, coord Coordinate) bool {
	moduleID := coord.Group + ":" + coord.Module
	for _, excluded := range repo.ExcludeModules {
		if excluded == moduleID {
			return false
		}
	}
	for _, excluded := range repo.ExcludeGroups {
		if excluded == coord.Group {
			return false
		}
	}
	for _, pattern := range repo.ExcludeGroupRegex {
		if regexp.MustCompile(pattern).MatchString(coord.Group) {
			return false
		}
	}

	hasIncludeRules := len(repo.IncludeModules) > 0 || len(repo.IncludeGroups) > 0 || len(repo.IncludeGroupRegex) > 0
	if !hasIncludeRules {
		return true
	}
	for _, included := range repo.IncludeModules {
		if included == moduleID {
			return true
		}
	}
	for _, included := range repo.IncludeGroups {
		if included == coord.Group {
			return true
		}
	}
	for _, pattern := range repo.IncludeGroupRegex {
		if regexp.MustCompile(pattern).MatchString(coord.Group) {
			return true
		}
	}
	return false
}
