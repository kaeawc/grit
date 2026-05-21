package gradlecache

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kaeawc/grit/internal/fsutil"
)

// StageByHardlink is a Stager that materializes sourcePath inside
// destDir using a hardlink when possible and falling back to a copy
// when the link can't be created (cross-device boundary, etc.). The
// staged file keeps the source's basename; destDir is treated as the
// final directory and must already be coordinate-unique.
var StageByHardlink Stager = StagerFunc(func(destDir, sourcePath string) (string, error) {
	if destDir == "" || sourcePath == "" {
		return "", errors.New("gradlecache: stage requires destDir and sourcePath")
	}
	dest := filepath.Join(destDir, filepath.Base(sourcePath))

	if err := os.Link(sourcePath, dest); err == nil || os.IsExist(err) {
		return dest, nil
	}

	if err := copyFile(sourcePath, dest); err != nil {
		return "", err
	}
	return dest, nil
})

func copyFile(src, dst string) error {
	in, err := os.Open(src) // #nosec G304 -- source path is owned by the artifact cache layout
	if err != nil {
		return fmt.Errorf("gradlecache: open source: %w", err)
	}
	defer func() { _ = in.Close() }()

	if err := fsutil.WriteFileAtomicStream(dst, 0o644, func(w io.Writer) error {
		_, err := io.Copy(w, in)
		return err
	}); err != nil {
		return fmt.Errorf("gradlecache: stage copy: %w", err)
	}
	return nil
}
