package dependencywiring

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/fsutil"
)

func copyBlobToFile(ctx context.Context, store cas.Store, hash cas.Hash, path string) error {
	rc, err := store.Get(ctx, hash)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.WriteFileAtomicStream(path, 0o644, func(w io.Writer) error {
		_, copyErr := io.Copy(w, rc)
		return copyErr
	})
}

func expandZipBlobToDir(ctx context.Context, store cas.Store, hash cas.Hash, dir string) error {
	rc, err := store.Get(ctx, hash)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, file := range zr.File {
		if file.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(filepath.Dir(dir), file.Name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.Create(target)
		if err != nil {
			_ = src.Close()
			return err
		}
		if _, err := io.Copy(dst, src); err != nil {
			_ = src.Close()
			_ = dst.Close()
			return err
		}
		_ = src.Close()
		if err := dst.Close(); err != nil {
			return err
		}
	}
	return nil
}
