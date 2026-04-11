package m2local

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/catalog"
	"github.com/kaeawc/grit/internal/modulebuild"
)

func (r *Resolver) moduleBasePath(coord Coordinate) string {
	return filepath.Join(r.CacheRoot, coord.Group, coord.Module, coord.Version)
}

func (r *Resolver) loadBOM(coord Coordinate) (map[string]string, error) {
	path := r.moduleBasePath(coord)
	pomFile, err := findFile(path, ".pom")
	if err != nil {
		return nil, err
	}
	return parseBOM(pomFile)
}

func (r *Resolver) seedPlatforms() map[string]map[string]string {
	platforms := map[string]map[string]string{}
	for _, lib := range r.Catalog.Libraries {
		version := lib.Version
		if version == "" && lib.VersionRef != "" {
			version = r.Catalog.Versions[lib.VersionRef]
		}
		if version == "" || !strings.Contains(lib.Name, "bom") {
			continue
		}
		managed, err := r.loadBOM(Coordinate{Group: lib.Group, Module: lib.Name, Version: version})
		if err != nil {
			continue
		}
		platforms[lib.Group+":"+lib.Name] = managed
	}
	return platforms
}

func (r *Resolver) expandRefs(refs []modulebuild.Ref, platforms map[string]map[string]string) ([]Coordinate, error) {
	var out []Coordinate
	for _, ref := range refs {
		switch ref.Kind {
		case "platform-raw":
			coord, err := parseRawCoordinate(ref.Value)
			if err != nil {
				return nil, err
			}
			managed, err := r.loadBOM(coord)
			if err != nil {
				continue
			}
			platforms[coord.Group+":"+coord.Module] = managed
		case "platform-library":
			lib, err := r.Catalog.ResolveLibrary(ref.Value)
			if err != nil {
				return nil, err
			}
			managed, err := r.loadBOM(Coordinate{Group: lib.Group, Module: lib.Name, Version: lib.Version})
			if err != nil {
				continue
			}
			platforms[lib.Group+":"+lib.Name] = managed
		case "library":
			lib, err := r.Catalog.ResolveLibrary(ref.Value)
			if err != nil {
				return nil, err
			}
			lib = normalizeResolvedLibrary(lib)
			version := lib.Version
			if version == "" {
				version = r.lookupVersion(platforms, lib.Group, lib.Name)
			}
			if version == "" {
				return nil, fmt.Errorf("no version for %s:%s", lib.Group, lib.Name)
			}
			out = append(out, r.normalizeRootCoordinate(Coordinate{Group: lib.Group, Module: lib.Name, Version: version}))
		case "raw":
			coord, err := parseRawCoordinate(ref.Value)
			if err != nil {
				return nil, err
			}
			out = append(out, r.normalizeRootCoordinate(coord))
		case "bundle":
			refs, err := r.Catalog.ResolveBundle(ref.Value)
			if err != nil {
				return nil, err
			}
			for _, item := range refs {
				lib, err := r.Catalog.ResolveLibrary(item)
				if err != nil {
					return nil, err
				}
				lib = normalizeResolvedLibrary(lib)
				version := lib.Version
				if version == "" {
					version = r.lookupVersion(platforms, lib.Group, lib.Name)
				}
				if version == "" {
					return nil, fmt.Errorf("no version for %s:%s", lib.Group, lib.Name)
				}
				out = append(out, r.normalizeRootCoordinate(Coordinate{Group: lib.Group, Module: lib.Name, Version: version}))
			}
		}
	}
	return out, nil
}

func (r *Resolver) normalizeRootCoordinate(coord Coordinate) Coordinate {
	if alt, ok := r.preferJVMSibling(coord); ok {
		return alt
	}
	return coord
}

func normalizeResolvedLibrary(lib catalog.Library) catalog.Library {
	if lib.Group == "io.mockk" && lib.Name == "mockk" {
		lib.Name = "mockk-jvm"
	}
	return lib
}

func parseRawCoordinate(value string) (Coordinate, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return Coordinate{}, fmt.Errorf("invalid raw coordinate %q", value)
	}
	group := strings.TrimSpace(parts[0])
	module := strings.TrimSpace(parts[1])
	version := strings.TrimSpace(parts[2])
	if group == "" || module == "" || version == "" {
		return Coordinate{}, fmt.Errorf("invalid raw coordinate %q", value)
	}
	return Coordinate{Group: group, Module: module, Version: version}, nil
}

func (r *Resolver) lookupVersion(platforms map[string]map[string]string, group, module string) string {
	if version := lookupManagedVersion(platforms, group, module); version != "" {
		return version
	}
	if strings.HasPrefix(group, "androidx.compose") {
		lib, err := r.Catalog.ResolveLibrary("compose.bom")
		if err == nil {
			managed, err := r.loadBOM(Coordinate{Group: lib.Group, Module: lib.Name, Version: lib.Version})
			if err == nil {
				return managed[group+":"+module]
			}
		}
	}
	if version := r.findCachedVersion(group, module); version != "" {
		return version
	}
	return ""
}

func (r *Resolver) findCachedVersion(group, module string) string {
	root := filepath.Join(r.CacheRoot, group, module)
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var versions []string
	for _, entry := range entries {
		if entry.IsDir() {
			versions = append(versions, entry.Name())
		}
	}
	sort.Strings(versions)
	if len(versions) == 0 {
		return ""
	}
	return versions[len(versions)-1]
}
