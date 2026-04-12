// Package lint defines the canonical shape of lint actions in grit's build
// model.
//
// A lint action runs Android Lint over a module variant. Every input is
// explicitly declared so the action hash is deterministic: identical inputs
// always produce the same cache key, enabling reliable result caching.
package lint

import (
	"encoding/json"
	"sort"

	"github.com/kaeawc/grit/internal/cas"
)

// Action is the declared shape of a lint invocation.
//
// Actions are value types. Two Actions that canonicalize to equal fields
// must produce the same CacheKey.
type Action struct {
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

// FileInput pairs a logical path with a content hash so that the cache key
// captures both identity and content.
type FileInput struct {
	Path string   `json:"path"`
	Hash cas.Hash `json:"hash"`
}

// CacheKey computes a deterministic hash over all declared inputs. The
// canonical encoding sorts unordered collections and uses a versioned JSON
// envelope so that future field additions naturally invalidate old keys.
func (a Action) CacheKey() cas.Hash {
	return cas.HashBytes(a.canonicalBytes())
}

func (a Action) canonicalBytes() []byte {
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

	c := canonicalAction{
		Version:          canonicalVersion,
		Sources:          sources,
		ResourceDirs:     resDirs,
		ManifestPath:     a.ManifestPath,
		CompileClasspath: classpath,
		LintRules:        rules,
		LintConfig:       a.LintConfig,
		Baseline:         a.Baseline,
		ToolVersion:      a.ToolVersion,
	}
	data, err := json.Marshal(c)
	if err != nil {
		panic("lint: canonical action failed to marshal: " + err.Error())
	}
	return data
}

const canonicalVersion = 1

type canonicalAction struct {
	Version          int         `json:"version"`
	Sources          []FileInput `json:"sources,omitempty"`
	ResourceDirs     []string    `json:"resourceDirs,omitempty"`
	ManifestPath     string      `json:"manifestPath,omitempty"`
	CompileClasspath []FileInput `json:"compileClasspath,omitempty"`
	LintRules        []FileInput `json:"lintRules,omitempty"`
	LintConfig       string      `json:"lintConfig,omitempty"`
	Baseline         string      `json:"baseline,omitempty"`
	ToolVersion      string      `json:"toolVersion"`
}
