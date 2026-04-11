package m2local

import (
	"os"
	"path/filepath"
)

func sharedMachineCacheRoot() string {
	home := os.Getenv("HOME")
	if home == "" {
		if fallback, err := os.UserHomeDir(); err == nil {
			home = fallback
		}
	}
	if home == "" {
		return filepath.Join(os.TempDir(), "grit")
	}
	return filepath.Join(home, ".grit")
}

func sharedResolveCacheRoot() string {
	return filepath.Join(sharedMachineCacheRoot(), "resolve")
}

func sharedAARCacheRoot() string {
	return filepath.Join(sharedMachineCacheRoot(), "aar")
}
