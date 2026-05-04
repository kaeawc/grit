package nativecompile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func outputsNewerThanInputs(outputPath string, inputs []string) bool {
	outputTime, ok := latestOutputModTime(outputPath)
	if !ok {
		return false
	}
	inputTime := latestInputModTime(inputs)
	if inputTime.IsZero() {
		return false
	}
	return !outputTime.Before(inputTime)
}

func latestOutputModTime(path string) (time.Time, bool) {
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		return info.ModTime(), true
	}
	var latest time.Time
	found := false
	filepath.WalkDir(path, func(walkPath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !found || info.ModTime().After(latest) {
			latest = info.ModTime()
			found = true
		}
		return nil
	})
	return latest, found
}

func latestInputModTime(inputs []string) time.Time {
	var latest time.Time
	for _, input := range inputs {
		info, err := os.Stat(input)
		if err != nil {
			continue
		}
		modTime := info.ModTime()
		if info.IsDir() {
			filepath.WalkDir(input, func(walkPath string, d os.DirEntry, walkErr error) error {
				if walkErr != nil || d.IsDir() {
					return nil
				}
				childInfo, err := d.Info()
				if err != nil {
					return nil
				}
				if childInfo.ModTime().After(modTime) {
					modTime = childInfo.ModTime()
				}
				return nil
			})
		}
		if modTime.After(latest) {
			latest = modTime
		}
	}
	return latest
}

func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.ReadFrom(in); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chmod(dst, info.Mode()); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target)
	})
}

func touchFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	now := time.Now()
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		return err
	}
	return os.Chtimes(path, now, now)
}

func ensureStampFromOutput(stampPath, outputPath string, inputs []string) bool {
	if pathIsFile(stampPath) || !outputsNewerThanInputs(outputPath, inputs) {
		return false
	}
	outputTime, ok := latestOutputModTime(outputPath)
	if !ok {
		return false
	}
	if err := touchFile(stampPath); err != nil {
		return false
	}
	_ = os.Chtimes(stampPath, outputTime, outputTime)
	return true
}

func pathIsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func pathIsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func writeFileIfChanged(path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeStamp(path, value string) error {
	return writeFileIfChanged(path, []byte(value+"\n"))
}

func stampMatches(path, value string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == value
}

func hasOutputFiles(path string) bool {
	_, ok := latestOutputModTime(path)
	return ok
}

func dexMergeStampValue(paths ...string) string {
	sum := sha256.New()
	for _, path := range paths {
		sum.Write([]byte(path))
		sum.Write([]byte{0})
		sum.Write([]byte(dirFingerprint(path)))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}
