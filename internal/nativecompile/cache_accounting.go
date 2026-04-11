package nativecompile

import (
	"io/fs"
	"path/filepath"
	"sort"
)

type CacheBucketAccounting struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

type CacheAccounting struct {
	Root    string                  `json:"root"`
	Buckets []CacheBucketAccounting `json:"buckets,omitempty"`
	Files   int                     `json:"files"`
	Bytes   int64                   `json:"bytes"`
}

var sharedNativeCacheBuckets = []string{
	"appcds",
	"apk/signed",
	"compile",
	"dex/app",
	"dex/external",
	"dex/project",
	"jvm-support/junit-platform-runner",
	"jvm-support/kotlin-test-shim",
	"junit-discovery",
	"module-snapshots",
	"resources/external",
	"resources/module-compile",
	"resources/module-symbols",
	"unit-test-resolve",
	"unit-test-run",
}

func SharedCacheAccounting() CacheAccounting {
	return cacheAccountingForRoot(sharedNativeCacheRoot())
}

func cacheAccountingForRoot(root string) CacheAccounting {
	accounting := CacheAccounting{
		Root:    root,
		Buckets: make([]CacheBucketAccounting, 0, len(sharedNativeCacheBuckets)),
	}
	for _, bucket := range sharedNativeCacheBuckets {
		path := filepath.Join(root, bucket)
		files, bytes := countFilesAndBytes(path)
		accounting.Buckets = append(accounting.Buckets, CacheBucketAccounting{
			Name:  bucket,
			Path:  path,
			Files: files,
			Bytes: bytes,
		})
		accounting.Files += files
		accounting.Bytes += bytes
	}
	sort.Slice(accounting.Buckets, func(i, j int) bool {
		return accounting.Buckets[i].Name < accounting.Buckets[j].Name
	})
	return accounting
}

func countFilesAndBytes(root string) (int, int64) {
	var files int
	var bytes int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		files++
		bytes += info.Size()
		return nil
	})
	return files, bytes
}
