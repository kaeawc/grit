package catalog

import (
	"fmt"
	"os"
	"strings"
)

type Catalog struct {
	Versions  map[string]string
	Libraries map[string]Library
	Bundles   map[string][]string
}

type Library struct {
	Group      string
	Name       string
	Version    string
	VersionRef string
}

func Load(path string) (*Catalog, error) {
	return LoadAll([]string{path})
}

func LoadAll(paths []string) (*Catalog, error) {
	c := &Catalog{
		Versions:  map[string]string{},
		Libraries: map[string]Library{},
		Bundles:   map[string][]string{},
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		section := ""
		lines := strings.Split(string(data), "\n")
		for i := 0; i < len(lines); i++ {
			raw := lines[i]
			line := stripInlineComment(strings.TrimSpace(raw))
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				section = strings.Trim(line, "[]")
				continue
			}

			switch section {
			case "versions":
				parseVersionLine(c, line)
			case "libraries":
				if err := parseLibraryLine(c, line); err != nil {
					return nil, err
				}
			case "bundles":
				if strings.Contains(line, "=") && strings.Contains(line, "[") && !strings.Contains(line, "]") {
					combined := line
					for i+1 < len(lines) {
						i++
						next := stripInlineComment(strings.TrimSpace(lines[i]))
						if next == "" || strings.HasPrefix(next, "#") {
							continue
						}
						combined += " " + next
						if strings.Contains(next, "]") {
							break
						}
					}
					parseBundleLine(c, combined)
					continue
				}
				parseBundleLine(c, line)
			}
		}
	}

	return c, nil
}

func (c *Catalog) ResolveLibrary(ref string) (Library, error) {
	lib, ok := c.Libraries[normalizeRef(ref)]
	if !ok {
		return Library{}, fmt.Errorf("unknown library ref %q", ref)
	}
	if lib.Version == "" && lib.VersionRef != "" {
		lib.Version = c.Versions[lib.VersionRef]
	}
	return lib, nil
}

func (c *Catalog) ResolveBundle(ref string) ([]string, error) {
	bundle, ok := c.Bundles[normalizeRef(ref)]
	if !ok {
		return nil, fmt.Errorf("unknown bundle ref %q", ref)
	}
	return bundle, nil
}

func parseVersionLine(c *Catalog, line string) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return
	}
	key := strings.TrimSpace(parts[0])
	value := stripInlineComment(strings.TrimSpace(parts[1]))
	c.Versions[key] = value
}

func parseLibraryLine(c *Catalog, line string) error {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return nil
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if !strings.HasPrefix(value, "{") {
		return nil
	}
	value = strings.Trim(value, "{}")

	lib := Library{}
	for _, field := range splitFields(value) {
		fieldParts := strings.SplitN(field, "=", 2)
		if len(fieldParts) != 2 {
			continue
		}
		fieldKey := strings.TrimSpace(fieldParts[0])
		fieldValue := stripInlineComment(strings.TrimSpace(fieldParts[1]))
		switch fieldKey {
		case "module":
			moduleParts := strings.SplitN(fieldValue, ":", 2)
			if len(moduleParts) != 2 {
				return fmt.Errorf("invalid module field %q", fieldValue)
			}
			lib.Group = moduleParts[0]
			lib.Name = moduleParts[1]
		case "group":
			lib.Group = fieldValue
		case "name":
			lib.Name = fieldValue
		case "version.ref":
			lib.VersionRef = fieldValue
		case "version":
			lib.Version = fieldValue
		}
	}
	c.Libraries[key] = lib
	return nil
}

func parseBundleLine(c *Catalog, line string) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	var refs []string
	for _, item := range strings.Split(value, ",") {
		item = stripInlineComment(strings.TrimSpace(item))
		if item != "" {
			refs = append(refs, item)
		}
	}
	c.Bundles[key] = refs
}

func splitFields(value string) []string {
	var fields []string
	var current strings.Builder
	inQuote := false
	for _, r := range value {
		switch r {
		case '"':
			inQuote = !inQuote
			current.WriteRune(r)
		case ',':
			if inQuote {
				current.WriteRune(r)
				continue
			}
			fields = append(fields, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if strings.TrimSpace(current.String()) != "" {
		fields = append(fields, strings.TrimSpace(current.String()))
	}
	return fields
}

func normalizeRef(ref string) string {
	return strings.ReplaceAll(ref, ".", "-")
}

func stripInlineComment(v string) string {
	if idx := strings.Index(v, "#"); idx >= 0 {
		v = v[:idx]
	}
	return strings.TrimSpace(strings.Trim(v, `"`))
}
