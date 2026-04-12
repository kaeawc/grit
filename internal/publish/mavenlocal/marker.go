package mavenlocal

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/lockfile"
)

const remoteRepositoriesMarkerName = "_remote.repositories"

func (p *Publisher) publishRemoteRepositoriesMarker(pin lockfile.Pin) error {
	payload, ok := remoteRepositoriesMarkerPayload(pin)
	if !ok {
		return nil
	}
	path := filepath.Join(p.moduleBasePath(pin.Coordinate), remoteRepositoriesMarkerName)
	return writeFileAtomically(path, payload)
}

func remoteRepositoriesMarkerPayload(pin lockfile.Pin) ([]byte, bool) {
	if pin.RepositoryID == "" {
		return nil, false
	}
	names := make([]string, 0, len(pin.Files))
	seen := map[string]struct{}{}
	for _, file := range pin.Files {
		if file.Name == "" {
			continue
		}
		if _, ok := seen[file.Name]; ok {
			continue
		}
		seen[file.Name] = struct{}{}
		names = append(names, file.Name)
	}
	if len(names) == 0 {
		return nil, false
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteString(">")
		b.WriteString(pin.RepositoryID)
		b.WriteString("=\n")
	}
	return []byte(b.String()), true
}
