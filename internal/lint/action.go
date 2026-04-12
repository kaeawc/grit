// Package lint defines the canonical shape of lint actions in grit's build
// model.
//
// A lint action runs Android Lint over a module variant. Every input is
// explicitly declared so the action hash is deterministic: identical inputs
// always produce the same cache key, enabling reliable result caching.
package lint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/cas"
)

// LintAction is the declared shape of a lint invocation.
//
// Actions are value types. Two actions with equal declared fields and equal
// on-disk contents for their path-based inputs must produce the same CacheKey.
type LintAction struct {
	// Sources is the set of source files to lint. Order is not part of
	// identity: sources are sorted during canonicalization.
	Sources []FileInput
	// ResourceDirs lists the resource directories (res/) to consider.
	// Order is not part of identity.
	ResourceDirs []string
	// ManifestPath is the path to the AndroidManifest.xml.
	ManifestPath string
	// CompileClasspath lists the compile classpath entries (jars/aars)
	// needed for type resolution. Order is not part of identity.
	CompileClasspath []FileInput
	// LintRules lists the custom lint rule jars. Order is not part of
	// identity.
	LintRules []FileInput
	// LintConfig is the path to the lint.xml configuration file.
	// Empty if no project-level config exists.
	LintConfig string
	// Baseline is the path to the lint baseline XML file.
	// Empty if no baseline is in use.
	Baseline string
	// ToolVersion is the version of the lint tool being executed.
	ToolVersion string
}

// Action preserves the original public name while the roadmap converges on the
// more explicit LintAction type name.
type Action = LintAction

// FileInput pairs a logical path with a content hash so that the cache key
// captures both identity and content.
type FileInput struct {
	Path string   `json:"path"`
	Hash cas.Hash `json:"hash"`
}

// CacheKey computes a deterministic hash over all declared inputs. The
// canonical encoding sorts unordered collections and uses a versioned JSON
// envelope so that future field additions naturally invalidate old keys.
func (a LintAction) CacheKey() cas.Hash {
	return cas.HashBytes(a.canonicalBytes())
}

func (a LintAction) canonicalBytes() []byte {
	sources := append([]FileInput(nil), a.Sources...)
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Path != sources[j].Path {
			return sources[i].Path < sources[j].Path
		}
		return sources[i].Hash.String() < sources[j].Hash.String()
	})

	classpath := append([]FileInput(nil), a.CompileClasspath...)
	sort.Slice(classpath, func(i, j int) bool {
		if classpath[i].Path != classpath[j].Path {
			return classpath[i].Path < classpath[j].Path
		}
		return classpath[i].Hash.String() < classpath[j].Hash.String()
	})

	rules := append([]FileInput(nil), a.LintRules...)
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Path != rules[j].Path {
			return rules[i].Path < rules[j].Path
		}
		return rules[i].Hash.String() < rules[j].Hash.String()
	})

	resDirs := append([]string(nil), a.ResourceDirs...)
	sort.Strings(resDirs)
	resourceInputs := make([]pathInput, 0, len(resDirs))
	for _, dir := range resDirs {
		if input := canonicalDirectoryInput(dir); input != nil {
			resourceInputs = append(resourceInputs, *input)
		}
	}

	c := canonicalAction{
		Version:          canonicalVersion,
		Sources:          sources,
		ResourceDirs:     resourceInputs,
		CompileClasspath: classpath,
		LintRules:        rules,
		ToolVersion:      a.ToolVersion,
	}
	c.Manifest = canonicalFileInput(a.ManifestPath)
	c.LintConfig = canonicalFileInput(a.LintConfig)
	c.Baseline = canonicalFileInput(a.Baseline)
	data, err := json.Marshal(c)
	if err != nil {
		panic("lint: canonical action failed to marshal: " + err.Error())
	}
	return data
}

const canonicalVersion = 2

type canonicalAction struct {
	Version          int         `json:"version"`
	Sources          []FileInput `json:"sources,omitempty"`
	ResourceDirs     []pathInput `json:"resourceDirs,omitempty"`
	Manifest         *pathInput  `json:"manifest,omitempty"`
	CompileClasspath []FileInput `json:"compileClasspath,omitempty"`
	LintRules        []FileInput `json:"lintRules,omitempty"`
	LintConfig       *pathInput  `json:"lintConfig,omitempty"`
	Baseline         *pathInput  `json:"baseline,omitempty"`
	ToolVersion      string      `json:"toolVersion"`
}

type pathInput struct {
	Path   string   `json:"path"`
	Hash   cas.Hash `json:"hash"`
	Exists bool     `json:"exists,omitempty"`
}

func canonicalFileInput(path string) *pathInput {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return &pathInput{Path: path}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return &pathInput{Path: path, Exists: true}
	}
	return &pathInput{
		Path:   path,
		Hash:   cas.HashBytes(data),
		Exists: true,
	}
}

func canonicalDirectoryInput(path string) *pathInput {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return &pathInput{Path: path}
	}
	files := directoryFileInputs(path)
	data, err := json.Marshal(struct {
		Version int         `json:"version"`
		Files   []FileInput `json:"files,omitempty"`
	}{
		Version: 1,
		Files:   files,
	})
	if err != nil {
		panic("lint: canonical resource directory failed to marshal: " + err.Error())
	}
	return &pathInput{
		Path:   path,
		Hash:   cas.HashBytes(data),
		Exists: true,
	}
}

func directoryFileInputs(root string) []FileInput {
	out := make([]FileInput, 0)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		out = append(out, FileInput{
			Path: path,
			Hash: cas.HashBytes(data),
		})
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Hash.String() < out[j].Hash.String()
	})
	return out
}
