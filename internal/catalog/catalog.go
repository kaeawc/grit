package catalog

import (
	"fmt"
	"os"
	"strings"
)

type Catalog struct {
	Versions     map[string]string
	RichVersions map[string]RichVersion
	Libraries    map[string]Library
	Bundles      map[string][]string
	Plugins      map[string]Plugin
	Provenance   map[string]Provenance
}

type Library struct {
	Group             string
	Name              string
	Version           string
	VersionRef        string
	VersionConstraint RichVersion
	Platform          bool
	SourceFile        string
	Alias             string
}

type Plugin struct {
	ID                string
	Version           string
	VersionRef        string
	VersionConstraint RichVersion
	SourceFile        string
	Alias             string
}

type RichVersion struct {
	Require   string
	Strictly  string
	Prefer    string
	Reject    []string
	RejectAll bool
	Ref       string
}

type Provenance struct {
	File    string
	Section string
	Alias   string
}

func Load(path string) (*Catalog, error) {
	return LoadAll([]string{path})
}

func LoadAll(paths []string) (*Catalog, error) {
	c := &Catalog{
		Versions:     map[string]string{},
		RichVersions: map[string]RichVersion{},
		Libraries:    map[string]Library{},
		Bundles:      map[string][]string{},
		Plugins:      map[string]Plugin{},
		Provenance:   map[string]Provenance{},
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
				parseVersionLine(c, path, line)
			case "libraries":
				if err := parseLibraryLine(c, path, line); err != nil {
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
					parseBundleLine(c, path, combined)
					continue
				}
				parseBundleLine(c, path, line)
			case "plugins":
				if err := parsePluginLine(c, path, line); err != nil {
					return nil, err
				}
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
	if lib.Version == "" {
		lib.Version = c.ResolveRichVersion(lib.VersionConstraint)
	}
	return lib, nil
}

func (c *Catalog) ResolveBundle(ref string) ([]string, error) {
	bundle, ok := c.Bundles[normalizeRef(ref)]
	if !ok {
		return nil, fmt.Errorf("unknown bundle ref %q", ref)
	}
	return append([]string(nil), bundle...), nil
}

func (c *Catalog) ResolvePlugin(ref string) (Plugin, error) {
	plugin, ok := c.Plugins[normalizeRef(ref)]
	if !ok {
		return Plugin{}, fmt.Errorf("unknown plugin ref %q", ref)
	}
	if plugin.Version == "" && plugin.VersionRef != "" {
		plugin.Version = c.Versions[plugin.VersionRef]
	}
	if plugin.Version == "" {
		plugin.Version = c.ResolveRichVersion(plugin.VersionConstraint)
	}
	return plugin, nil
}

func (c *Catalog) ResolveRichVersion(v RichVersion) string {
	switch {
	case strings.TrimSpace(v.Strictly) != "":
		return v.Strictly
	case strings.TrimSpace(v.Require) != "":
		return v.Require
	case strings.TrimSpace(v.Prefer) != "":
		return v.Prefer
	case strings.TrimSpace(v.Ref) != "":
		return c.Versions[v.Ref]
	default:
		return ""
	}
}

func (c *Catalog) ProvenanceFor(section, ref string) Provenance {
	if c == nil {
		return Provenance{}
	}
	key := section + ":" + normalizeRef(ref)
	return c.Provenance[key]
}

func parseVersionLine(c *Catalog, path, line string) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return
	}
	key := strings.TrimSpace(parts[0])
	value := stripInlineComment(strings.TrimSpace(parts[1]))
	if strings.HasPrefix(value, "{") {
		rich := parseRichVersion(strings.Trim(value, "{}"))
		c.RichVersions[key] = rich
		c.Versions[key] = c.ResolveRichVersion(rich)
	} else {
		c.Versions[key] = value
	}
	recordProvenance(c, path, "versions", key)
}

func parseLibraryLine(c *Catalog, path, line string) error {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return nil
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if !strings.HasPrefix(value, "{") {
		value = stripInlineComment(value)
		coordParts := strings.Split(value, ":")
		if len(coordParts) == 3 {
			c.Libraries[normalizeRef(key)] = Library{Group: coordParts[0], Name: coordParts[1], Version: coordParts[2], Alias: key, SourceFile: path}
			recordProvenance(c, path, "libraries", key)
		}
		return nil
	}
	value = strings.Trim(value, "{}")

	lib := Library{Alias: key, SourceFile: path}
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
			if strings.HasPrefix(strings.TrimSpace(fieldParts[1]), "{") {
				lib.VersionConstraint = parseRichVersion(strings.Trim(strings.TrimSpace(fieldParts[1]), "{}"))
			} else {
				lib.Version = fieldValue
			}
		case "platform":
			lib.Platform = strings.EqualFold(fieldValue, "true")
		}
	}
	if lib.Version == "" && lib.VersionRef != "" {
		lib.Version = c.Versions[lib.VersionRef]
	}
	if lib.Version == "" {
		lib.Version = c.ResolveRichVersion(lib.VersionConstraint)
	}
	c.Libraries[normalizeRef(key)] = lib
	recordProvenance(c, path, "libraries", key)
	return nil
}

func parseBundleLine(c *Catalog, path, line string) {
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
	c.Bundles[normalizeRef(key)] = refs
	recordProvenance(c, path, "bundles", key)
}

func parsePluginLine(c *Catalog, path, line string) error {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return nil
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if !strings.HasPrefix(value, "{") {
		return nil
	}
	plugin := Plugin{Alias: key, SourceFile: path}
	value = strings.Trim(value, "{}")
	for _, field := range splitFields(value) {
		fieldParts := strings.SplitN(field, "=", 2)
		if len(fieldParts) != 2 {
			continue
		}
		fieldKey := strings.TrimSpace(fieldParts[0])
		fieldValue := stripInlineComment(strings.TrimSpace(fieldParts[1]))
		switch fieldKey {
		case "id":
			plugin.ID = fieldValue
		case "version.ref":
			plugin.VersionRef = fieldValue
		case "version":
			if strings.HasPrefix(strings.TrimSpace(fieldParts[1]), "{") {
				plugin.VersionConstraint = parseRichVersion(strings.Trim(strings.TrimSpace(fieldParts[1]), "{}"))
			} else {
				plugin.Version = fieldValue
			}
		}
	}
	if plugin.Version == "" && plugin.VersionRef != "" {
		plugin.Version = c.Versions[plugin.VersionRef]
	}
	if plugin.Version == "" {
		plugin.Version = c.ResolveRichVersion(plugin.VersionConstraint)
	}
	c.Plugins[normalizeRef(key)] = plugin
	recordProvenance(c, path, "plugins", key)
	return nil
}

func parseRichVersion(value string) RichVersion {
	out := RichVersion{}
	for _, field := range splitFields(value) {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		raw := strings.TrimSpace(parts[1])
		fieldValue := stripInlineComment(raw)
		switch key {
		case "require":
			out.Require = fieldValue
		case "strictly":
			out.Strictly = fieldValue
		case "prefer":
			out.Prefer = fieldValue
		case "version.ref":
			out.Ref = fieldValue
		case "reject":
			out.Reject = parseStringArray(raw)
		case "rejectAll":
			out.RejectAll = strings.EqualFold(fieldValue, "true")
		}
	}
	return out
}

func parseStringArray(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	var out []string
	for _, item := range strings.Split(value, ",") {
		item = stripInlineComment(strings.TrimSpace(item))
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func recordProvenance(c *Catalog, path, section, alias string) {
	if c == nil {
		return
	}
	if c.Provenance == nil {
		c.Provenance = map[string]Provenance{}
	}
	c.Provenance[section+":"+normalizeRef(alias)] = Provenance{File: path, Section: section, Alias: alias}
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
