package nativecompile

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/kaeawc/grit/internal/catalog"
	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

type compileState struct {
	mu                sync.Mutex
	inFlight          map[string]*inFlightCompile
	outputs           map[string]compiledModule
	externalResources map[string]androidResourceArtifact
	depGraph          map[string]map[string]struct{}
	parsedDeps        map[string]*modulebuild.Dependencies
	catalogOnce       sync.Once
	catalog           *catalog.Catalog
	resolver          *m2local.Resolver
	catalogErr        error
	toolchainOnce     sync.Once
	toolchain         *kotlinToolchain
	toolchainErr      error
}

type inFlightCompile struct {
	done chan struct{}
	err  error
}

type androidResourceArtifact struct {
	ModulePath    string
	Namespace     string
	ManifestPath  string
	CompiledDir   string
	CompiledFiles []string
	CompiledStamp string
	SymbolJar     string
}

type compiledModule struct {
	classesDir       string
	runtimeInputs    []string
	androidResources []androidResourceArtifact
}

type moduleSnapshot struct {
	RuntimeInputs    []string                  `json:"runtimeInputs"`
	AndroidResources []androidResourceArtifact `json:"androidResources"`
	DirectDepStamps  map[string]string         `json:"directDepStamps"`
}

func newCompileState() *compileState {
	return &compileState{
		inFlight:          map[string]*inFlightCompile{},
		outputs:           map[string]compiledModule{},
		externalResources: map[string]androidResourceArtifact{},
		depGraph:          map[string]map[string]struct{}{},
		parsedDeps:        map[string]*modulebuild.Dependencies{},
	}
}

func (s *compileState) addProjectDeps(parent string, children []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, child := range children {
		if child == parent {
			return fmt.Errorf("cyclic project dependency while compiling %s", parent)
		}
		if s.reachesLocked(child, parent) {
			return fmt.Errorf("cyclic project dependency between %s and %s", parent, child)
		}
	}
	if _, ok := s.depGraph[parent]; !ok {
		s.depGraph[parent] = map[string]struct{}{}
	}
	for _, child := range children {
		s.depGraph[parent][child] = struct{}{}
	}
	return nil
}

func (s *compileState) reachesLocked(from, target string) bool {
	if from == target {
		return true
	}
	seen := map[string]bool{}
	queue := []string{from}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if seen[node] {
			continue
		}
		seen[node] = true
		for next := range s.depGraph[node] {
			if next == target {
				return true
			}
			if !seen[next] {
				queue = append(queue, next)
			}
		}
	}
	return false
}

func (s *compileState) dependenciesForModule(buildFile string) (*modulebuild.Dependencies, error) {
	s.mu.Lock()
	if deps, ok := s.parsedDeps[buildFile]; ok {
		s.mu.Unlock()
		return deps, nil
	}
	s.mu.Unlock()

	deps, err := modulebuild.ParseDependencies(buildFile)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if existing, ok := s.parsedDeps[buildFile]; ok {
		s.mu.Unlock()
		return existing, nil
	}
	s.parsedDeps[buildFile] = deps
	s.mu.Unlock()
	return deps, nil
}

func (s *compileState) resolverForProject(prj *project.Project) (*m2local.Resolver, error) {
	s.catalogOnce.Do(func() {
		if len(prj.VersionCatalogs) == 0 {
			s.catalog = &catalog.Catalog{
				Versions:  map[string]string{},
				Libraries: map[string]catalog.Library{},
				Bundles:   map[string][]string{},
			}
		} else {
			s.catalog, s.catalogErr = catalog.LoadAll(prj.VersionCatalogs)
			if s.catalogErr != nil {
				return
			}
		}
		s.resolver = m2local.New(filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1"), prj.RootDir, prj.Repositories, s.catalog)
	})
	if s.catalogErr != nil {
		return nil, s.catalogErr
	}
	return s.resolver, nil
}
