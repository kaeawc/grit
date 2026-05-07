package m2local

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kaeawc/grit/internal/griterr"
	"github.com/kaeawc/grit/internal/pathutil"
	"github.com/kaeawc/grit/internal/project"
)

var ErrOffline = errors.New("m2local: offline mode")

func (r *Resolver) fetchPOM(coord Coordinate) (string, error) {
	artifactVersion := r.snapshotArtifactVersion(coord, "pom")
	relPath := strings.ReplaceAll(coord.Group, ".", "/") + "/" + coord.Module + "/" + coord.Version + "/" + coord.Module + "-" + artifactVersion + ".pom"
	outPath := filepath.Join(r.WorkRoot, ".grit", "metadata", coord.Group, coord.Module, coord.Version, coord.Module+"-"+coord.Version+".pom")
	return r.fetchRemoteFile(coord, relPath, outPath, "pom")
}

func (r *Resolver) fetchModuleMetadata(coord Coordinate) (string, error) {
	artifactVersion := r.snapshotArtifactVersion(coord, "module")
	relPath := strings.ReplaceAll(coord.Group, ".", "/") + "/" + coord.Module + "/" + coord.Version + "/" + coord.Module + "-" + artifactVersion + ".module"
	outPath := filepath.Join(r.WorkRoot, ".grit", "metadata", coord.Group, coord.Module, coord.Version, coord.Module+"-"+coord.Version+".module")
	return r.fetchRemoteFile(coord, relPath, outPath, "module metadata")
}

func (r *Resolver) fetchArtifact(coord Coordinate, ext string) (string, error) {
	if ext != ".jar" && ext != ".aar" {
		return "", griterr.Newf(griterr.ErrUnsupported, "artifact extension %q", ext)
	}
	artifactVersion := r.snapshotArtifactVersion(coord, strings.TrimPrefix(ext, "."))
	relPath := strings.ReplaceAll(coord.Group, ".", "/") + "/" + coord.Module + "/" + coord.Version + "/" + coord.Module + "-" + artifactVersion + ext
	outPath := filepath.Join(r.moduleBasePath(coord), "downloaded", coord.Module+"-"+coord.Version+ext)
	return r.fetchRemoteFile(coord, relPath, outPath, ext[1:])
}

type snapshotMetadata struct {
	Versioning struct {
		SnapshotVersions []struct {
			Extension string `xml:"extension"`
			Value     string `xml:"value"`
		} `xml:"snapshotVersions>snapshotVersion"`
		Snapshot struct {
			Timestamp   string `xml:"timestamp"`
			BuildNumber string `xml:"buildNumber"`
		} `xml:"snapshot"`
	} `xml:"versioning"`
}

func (r *Resolver) snapshotArtifactVersion(coord Coordinate, ext string) string {
	if !strings.HasSuffix(coord.Version, "-SNAPSHOT") {
		return coord.Version
	}
	if r.Offline {
		return coord.Version
	}
	relPath := strings.ReplaceAll(coord.Group, ".", "/") + "/" + coord.Module + "/" + coord.Version + "/maven-metadata.xml"
	for _, baseURL := range r.remoteRepositoryURLs(coord) {
		resp, err := http.Get(baseURL + relPath)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			continue
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil || len(body) == 0 {
			continue
		}
		var metadata snapshotMetadata
		if xml.Unmarshal(body, &metadata) != nil {
			continue
		}
		for _, version := range metadata.Versioning.SnapshotVersions {
			if version.Extension == ext && version.Value != "" {
				return version.Value
			}
		}
		if metadata.Versioning.Snapshot.Timestamp != "" && metadata.Versioning.Snapshot.BuildNumber != "" {
			base := strings.TrimSuffix(coord.Version, "-SNAPSHOT")
			return base + "-" + metadata.Versioning.Snapshot.Timestamp + "-" + metadata.Versioning.Snapshot.BuildNumber
		}
	}
	return coord.Version
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
	if r.Offline {
		return "", fmt.Errorf("%w: cannot fetch %s for %s", ErrOffline, label, coordinateID(coord))
	}
	if r.negativeCacheHit(coord, relPath, label) {
		return "", fmt.Errorf("%s not found for %s (cached negative repository lookup)", label, coordinateID(coord))
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
			_ = resp.Body.Close()
			continue
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return "", err
		}
		if err := verifyChecksumIfAvailable(baseURL+relPath, body); err != nil {
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
	_ = r.recordNegativeCache(coord, relPath, label)
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
	if len(urls) == 0 && r.AllowRepositoryFallback {
		urls = append(urls, "https://dl.google.com/dl/android/maven2/", "https://repo1.maven.org/maven2/")
	}
	return mergeUnique(nil, urls)
}

func (r *Resolver) negativeCachePath(coord Coordinate, relPath, label string) string {
	key := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(coordinateID(coord) + "_" + label + "_" + relPath)
	return filepath.Join(r.WorkRoot, ".grit", "metadata", "missing", key+".missing")
}

func (r *Resolver) negativeCacheHit(coord Coordinate, relPath, label string) bool {
	path := r.negativeCachePath(coord, relPath, label)
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < 24*time.Hour
}

func (r *Resolver) recordNegativeCache(coord Coordinate, relPath, label string) error {
	path := r.negativeCachePath(coord, relPath, label)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(coordinateID(coord)+" "+label+"\n"), 0o644)
}

func verifyChecksumIfAvailable(url string, body []byte) error {
	if len(body) == 0 {
		return nil
	}
	for _, suffix := range []string{".sha256", ".sha1"} {
		resp, err := http.Get(url + suffix)
		if err != nil {
			continue
		}
		checksum, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			continue
		}
		fields := strings.Fields(string(checksum))
		if len(fields) == 0 {
			continue
		}
		switch suffix {
		case ".sha256":
			sum := sha256.Sum256(body)
			if !strings.EqualFold(hex.EncodeToString(sum[:]), fields[0]) {
				return fmt.Errorf("checksum mismatch for %s", url)
			}
		case ".sha1":
			sum := sha1.Sum(body)
			if !strings.EqualFold(hex.EncodeToString(sum[:]), fields[0]) {
				return fmt.Errorf("checksum mismatch for %s", url)
			}
		}
		return nil
	}
	return nil
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
