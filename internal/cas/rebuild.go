package cas

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// RebuildReport summarises the result of a RebuildIndexes call.
type RebuildReport struct {
	BlobsScanned        int `json:"blobsScanned"`
	ProvenanceRestored  int `json:"provenanceRestored"`
	ProvenanceUnchanged int `json:"provenanceUnchanged"`
}

// RebuildIndexes walks blobs/ under store and regenerates the provenance/
// index for any blob whose provenance record is missing. Existing provenance
// records are left untouched (first-writer-wins). Returns an error if any
// blob path is malformed; missing provenance for a valid blob is repaired,
// not reported.
//
// Restored provenance records use the blob file's mtime as CreatedAt and
// SourceImport with a "rebuilt-by-RebuildIndexes" note so callers can tell
// recovered records apart from real provenance.
func RebuildIndexes(ctx context.Context, store *FilesystemStore) (RebuildReport, error) {
	if store == nil {
		return RebuildReport{}, errors.New("cas: RebuildIndexes: nil store")
	}
	if err := ctx.Err(); err != nil {
		return RebuildReport{}, err
	}
	blobRoot := filepath.Join(store.root, "blobs")
	var report RebuildReport
	err := filepath.WalkDir(blobRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if name := d.Name(); len(name) > 0 && name[0] == '.' {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(blobRoot, path)
		if err != nil {
			return err
		}
		dir := filepath.Dir(rel)
		base := filepath.Base(rel)
		if dir == "." {
			return fmt.Errorf("cas: RebuildIndexes: blob %q is not in <hh>/<rest> layout", rel)
		}
		hexStr := dir + base
		h, err := ParseHash(hexStr)
		if err != nil {
			return fmt.Errorf("cas: RebuildIndexes: %w", err)
		}
		report.BlobsScanned++

		provPath := store.provenancePath(h)
		if _, statErr := os.Stat(provPath); statErr == nil {
			report.ProvenanceUnchanged++
			return nil
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return statErr
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		stub := Provenance{
			Source: Source{
				Kind:   SourceImport,
				Import: &ImportSource{Note: "rebuilt-by-RebuildIndexes"},
			},
			CreatedAt: info.ModTime().UTC(),
		}
		if err := store.writeProvenanceIfMissing(h, stub); err != nil {
			return err
		}
		report.ProvenanceRestored++
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return report, nil
	}
	return report, err
}
