package m2local

import (
	"fmt"
	"os"
	"path/filepath"
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
		fetched, fetchErr := r.fetchPOM(coord)
		if fetchErr != nil {
			return nil, err
		}
		pomFile = fetched
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
			if coord, ok := r.composeAccessorCoordinate(ref.Value, platforms); ok {
				out = append(out, r.normalizeRootCoordinate(coord))
				continue
			}
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

func (r *Resolver) composeAccessorCoordinate(value string, platforms map[string]map[string]string) (Coordinate, bool) {
	group, module, ok := ComposeAccessorModule(value)
	if !ok {
		return Coordinate{}, false
	}
	version := ""
	if strings.HasPrefix(group, "androidx.compose") {
		version = r.lookupVersion(platforms, group, module)
	}
	if r != nil && r.Catalog != nil {
		if version == "" {
			for _, key := range []string{"compose-multiplatform", "composeMultiplatform", "compose"} {
				if v := strings.TrimSpace(r.Catalog.Versions[key]); v != "" {
					version = v
					break
				}
			}
		}
	}
	if version == "" {
		version = r.lookupVersion(platforms, group, module)
	}
	if version == "" {
		return Coordinate{}, false
	}
	return Coordinate{Group: group, Module: module, Version: version}, true
}

func ComposeAccessorModule(value string) (string, string, bool) {
	switch strings.TrimSpace(value) {
	case "compose.ui":
		return "org.jetbrains.compose.ui", "ui", true
	case "compose.runtime":
		return "org.jetbrains.compose.runtime", "runtime", true
	case "compose.foundation":
		return "org.jetbrains.compose.foundation", "foundation", true
	case "compose.material3":
		return "androidx.compose.material3", "material3-android", true
	case "compose.components.resources":
		return "org.jetbrains.compose.components", "components-resources", true
	case "compose.uiTest":
		return "org.jetbrains.compose.ui", "ui-test", true
	default:
		return "", "", false
	}
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
	var best string
	for _, entry := range entries {
		if entry.IsDir() {
			version := entry.Name()
			if best == "" || compareVersionStrings(version, best) > 0 {
				best = version
			}
		}
	}
	return best
}

func compareVersionStrings(a, b string) int {
	at := tokenizeVersion(a)
	bt := tokenizeVersion(b)
	for i := 0; i < len(at) || i < len(bt); i++ {
		if i >= len(at) {
			switch bt[i].kind {
			case versionTokenNumeric:
				if bt[i].numericIsZero() {
					continue
				}
				return -1
			case versionTokenQualifier:
				switch cmp := compareMissingWithQualifier(bt[i]); cmp {
				case 0:
					continue
				default:
					return cmp
				}
			}
		}
		if i >= len(bt) {
			switch at[i].kind {
			case versionTokenNumeric:
				if at[i].numericIsZero() {
					continue
				}
				return 1
			case versionTokenQualifier:
				switch cmp := compareMissingWithQualifier(at[i]); cmp {
				case 0:
					continue
				default:
					return -cmp
				}
			}
		}
		if cmp := compareVersionToken(at[i], bt[i]); cmp != 0 {
			return cmp
		}
	}
	if a == b {
		return 0
	}
	if len(a) != len(b) {
		if len(a) < len(b) {
			return 1
		}
		return -1
	}
	if a < b {
		return -1
	}
	return 1
}

type versionTokenKind int

const (
	versionTokenNumeric versionTokenKind = iota
	versionTokenQualifier
)

type versionToken struct {
	kind      versionTokenKind
	numeric   string
	qualifier string
	suffix    string
}

func (t versionToken) numericIsZero() bool {
	return strings.TrimLeft(t.numeric, "0") == ""
}

func tokenizeVersion(v string) []versionToken {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return nil
	}
	parts := strings.FieldsFunc(v, func(r rune) bool {
		return r == '.' || r == '-' || r == '_'
	})
	tokens := make([]versionToken, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if isDigits(part) {
			tokens = append(tokens, versionToken{kind: versionTokenNumeric, numeric: part})
			continue
		}
		prefix, suffix, ok := splitQualifierSuffix(part)
		if !ok {
			tokens = append(tokens, versionToken{kind: versionTokenQualifier, qualifier: part})
			continue
		}
		tokens = append(tokens, versionToken{kind: versionTokenQualifier, qualifier: prefix, suffix: suffix})
	}
	return tokens
}

func compareVersionToken(a, b versionToken) int {
	if a.kind != b.kind {
		if a.kind == versionTokenNumeric {
			return 1
		}
		return -1
	}
	switch a.kind {
	case versionTokenNumeric:
		return compareNumericStrings(a.numeric, b.numeric)
	case versionTokenQualifier:
		ar := qualifierRank(a.qualifier)
		br := qualifierRank(b.qualifier)
		if ar != br {
			if ar < br {
				return -1
			}
			return 1
		}
		if cmp := compareNumericStrings(a.suffix, b.suffix); cmp != 0 {
			return cmp
		}
		if a.qualifier == b.qualifier {
			return 0
		}
		if a.qualifier < b.qualifier {
			return -1
		}
		return 1
	default:
		return 0
	}
}

func compareMissingWithQualifier(tok versionToken) int {
	if tok.kind != versionTokenQualifier {
		return 0
	}
	rank := qualifierRank(tok.qualifier)
	switch {
	case rank < 60:
		return 1
	case rank > 60:
		return -1
	default:
		return 0
	}
}

func compareNumericStrings(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if a == "" {
		a = "0"
	}
	if b == "" {
		b = "0"
	}
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	if a == b {
		return 0
	}
	if a < b {
		return -1
	}
	return 1
}

func qualifierRank(q string) int {
	switch q {
	case "", "ga", "final", "release":
		return 60
	case "alpha", "a":
		return 10
	case "beta", "b":
		return 20
	case "milestone", "m":
		return 30
	case "cr", "rc":
		return 40
	case "snapshot":
		return 50
	case "sp":
		return 70
	default:
		return 55
	}
}

func splitQualifierSuffix(part string) (string, string, bool) {
	i := 0
	for i < len(part) && isAlpha(part[i]) {
		i++
	}
	if i == 0 || i == len(part) {
		return "", "", false
	}
	j := i
	for j < len(part) && isDigit(part[j]) {
		j++
	}
	if j != len(part) {
		return "", "", false
	}
	return part[:i], part[i:], true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return true
}

func isAlpha(b byte) bool {
	return b >= 'a' && b <= 'z'
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
