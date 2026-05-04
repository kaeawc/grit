package m2local

import (
	"os"
)

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func existingFile(path string) string {
	if fileExists(path) {
		return path
	}
	return ""
}

func existingDir(path string) string {
	if dirExists(path) {
		return path
	}
	return ""
}
