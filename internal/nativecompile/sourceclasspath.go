package nativecompile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kaeawc/grit/internal/dependencywiring"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

func collectJavaSources(root string) ([]string, error) {
	var sources []string
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".java") {
			sources = append(sources, path)
		}
		return nil
	})
	sort.Strings(sources)
	return sources, err
}

func flattenCompiledResourceFiles(resources ...[]androidResourceArtifact) []string {
	var files []string
	for _, set := range resources {
		for _, artifact := range set {
			files = append(files, artifact.CompiledFiles...)
		}
	}
	return mergePaths(files)
}

func resourceArtifactStamps(resources ...[]androidResourceArtifact) []string {
	var files []string
	for _, set := range resources {
		for _, artifact := range set {
			switch {
			case artifact.CompiledStamp != "":
				files = append(files, artifact.CompiledStamp)
			case artifact.CompiledDir != "":
				files = append(files, artifact.CompiledDir)
			}
		}
	}
	return mergePaths(files)
}

func appendResourceArtifacts(existing []androidResourceArtifact, artifact androidResourceArtifact) []androidResourceArtifact {
	return uniqueResourceArtifacts(append(existing, artifact))
}

func uniqueResourceArtifacts(resources []androidResourceArtifact) []androidResourceArtifact {
	seen := map[string]bool{}
	var out []androidResourceArtifact
	for _, artifact := range resources {
		if artifact.ModulePath == "" || seen[artifact.ModulePath] {
			continue
		}
		seen[artifact.ModulePath] = true
		out = append(out, artifact)
	}
	return out
}

func androidJarPath() string {
	return filepath.Join(androidSDKRoot(), "platforms", "android-36", "android.jar")
}

func androidSDKRoot() string {
	if root := strings.TrimSpace(os.Getenv("ANDROID_HOME")); root != "" {
		return root
	}
	if root := strings.TrimSpace(os.Getenv("ANDROID_SDK_ROOT")); root != "" {
		return root
	}
	return filepath.Join(os.Getenv("HOME"), "Library", "Android", "sdk")
}

func sanitizePathComponent(s string) string {
	replacer := strings.NewReplacer(":", "_", "/", "_", "\\", "_", "@", "_")
	return replacer.Replace(s)
}

func countNonEmptyLines(s string) int {
	count := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func collectSources(root string) ([]string, error) {
	var sources []string
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".kt") || strings.HasSuffix(path, ".java") {
			sources = append(sources, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(sources)
	return sources, nil
}

func discoverJUnitTests(root string) ([]string, error) {
	rePackage := regexp.MustCompile(`(?m)^package\s+([A-Za-z0-9\._]+)`)
	reClass := regexp.MustCompile(`(?m)^\s*class\s+([A-Za-z0-9_]+Test)\b`)
	var out []string
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".kt") {
			return err
		}
		data, err := os.ReadFile(path) // #nosec
		if err != nil {
			return err
		}
		body := string(data)
		pkg := ""
		if match := rePackage.FindStringSubmatch(body); len(match) > 1 {
			pkg = match[1]
		}
		for _, match := range reClass.FindAllStringSubmatch(body, -1) {
			name := match[1]
			if pkg != "" {
				out = append(out, pkg+"."+name)
			} else {
				out = append(out, name)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func discoverJUnitTestsCached(root string, excludeAutoMobile bool) ([]string, error) {
	cachePath := junitDiscoveryCachePath(root, excludeAutoMobile)
	if pathIsFile(cachePath) {
		data, err := os.ReadFile(cachePath)
		if err == nil {
			var cached []string
			if jsonErr := json.Unmarshal(data, &cached); jsonErr == nil {
				return cached, nil
			}
		}
	}
	discovered, err := discoverJUnitTests(root)
	if err != nil {
		return nil, err
	}
	if excludeAutoMobile {
		discovered = filterAutoMobile(discovered)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err == nil {
		if data, jsonErr := json.Marshal(discovered); jsonErr == nil {
			_ = os.WriteFile(cachePath, data, 0o644)
		}
	}
	return discovered, nil
}

func junitDiscoveryCachePath(root string, excludeAutoMobile bool) string {
	sum := sha256.New()
	sum.Write([]byte("junit-discovery-v1"))
	sum.Write([]byte{0})
	sum.Write([]byte(filepath.Clean(root)))
	sum.Write([]byte{0})
	sum.Write([]byte(cacheIdentityForInput(root)))
	sum.Write([]byte{0})
	if excludeAutoMobile {
		sum.Write([]byte("exclude-automobile"))
	}
	return filepath.Join(sharedNativeCacheRoot(), "junit-discovery", hex.EncodeToString(sum.Sum(nil))+".json")
}

func filterAutoMobile(classes []string) []string {
	var out []string
	for _, class := range classes {
		if strings.Contains(class, ".automobiletest.") {
			continue
		}
		out = append(out, class)
	}
	return out
}

func mergePaths(parts ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, part := range parts {
		for _, item := range part {
			if item == "" || seen[item] {
				continue
			}
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

func resolveLocalDependencyRefs(prj *project.Project, mod *project.Module, refs []modulebuild.Ref) []string {
	var out []string
	for _, ref := range refs {
		if ref.Kind != "raw" {
			continue
		}
		out = append(out, resolveRawDependencyRef(prj, mod, ref.Value)...)
	}
	return mergePaths(out)
}

func resolveRawDependencyRef(prj *project.Project, mod *project.Module, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.Contains(value, ":") && !strings.Contains(value, string(os.PathSeparator)) && !strings.HasPrefix(value, ".") && !strings.HasPrefix(value, "$") {
		return nil
	}
	if strings.HasPrefix(value, "dir = ") {
		dir := strings.Trim(strings.TrimSpace(strings.TrimPrefix(value, "dir = ")), `"`)
		dir = expandDependencyPath(prj, mod, dir)
		matches, _ := filepath.Glob(filepath.Join(dir, "*.jar"))
		sort.Strings(matches)
		return matches
	}
	path := expandDependencyPath(prj, mod, value)
	if strings.ContainsAny(path, "*?[") {
		matches, _ := filepath.Glob(path)
		sort.Strings(matches)
		return matches
	}
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			matches, _ := filepath.Glob(filepath.Join(path, "*.jar"))
			sort.Strings(matches)
			return matches
		}
		return []string{path}
	}
	return []string{path}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func expandDependencyPath(prj *project.Project, mod *project.Module, value string) string {
	replacements := map[string]string{
		"$androidSdkPath":                      androidSDKRoot(),
		"$compileSdk":                          firstNonEmpty(mod.CompileSDK, "36"),
		"$buildToolsVersion":                   firstNonEmpty(mod.BuildToolsVersion, "36.0.0"),
		"$minSdk":                              firstNonEmpty(mod.MinSDK, "24"),
		"${System.getProperty(\"user.home\")}": os.Getenv("HOME"),
	}
	for needle, replacement := range replacements {
		value = strings.ReplaceAll(value, needle, replacement)
	}
	value = strings.Trim(strings.TrimSpace(value), `"`)
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(mod.Dir, value))
}

func excludePath(paths []string, target string) []string {
	var out []string
	for _, path := range paths {
		if path != target {
			out = append(out, path)
		}
	}
	return out
}

func collapseVersions(paths []string) []string {
	paths = filterEmptyListenableFuture(paths)
	paths = filterKnownRuntimeDuplicates(paths)
	type picked struct {
		version string
		path    string
	}
	byKey := map[string]picked{}
	var passthrough []string
	for _, path := range paths {
		key, version, ok := artifactKeyVersion(path)
		if !ok {
			passthrough = append(passthrough, path)
			continue
		}
		prev, exists := byKey[key]
		if !exists || compareVersion(version, prev.version) > 0 {
			byKey[key] = picked{version: version, path: path}
		}
	}
	var out []string
	out = append(out, passthrough...)
	for _, item := range byKey {
		out = append(out, item.path)
	}
	sort.Strings(out)
	return out
}

func filterKnownRuntimeDuplicates(paths []string) []string {
	hasLiveDataCore := false
	for _, path := range paths {
		if strings.Contains(path, string(os.PathSeparator)+"androidx.lifecycle"+string(os.PathSeparator)+"lifecycle-livedata-core"+string(os.PathSeparator)) {
			hasLiveDataCore = true
			break
		}
	}
	if !hasLiveDataCore {
		return paths
	}
	var out []string
	for _, path := range paths {
		if strings.Contains(path, string(os.PathSeparator)+"androidx.lifecycle"+string(os.PathSeparator)+"lifecycle-livedata-core-ktx"+string(os.PathSeparator)) {
			continue
		}
		out = append(out, path)
	}
	return out
}

func filterEmptyListenableFuture(paths []string) []string {
	hasGuava := false
	for _, path := range paths {
		if strings.Contains(path, string(os.PathSeparator)+"com.google.guava"+string(os.PathSeparator)+"guava"+string(os.PathSeparator)) {
			hasGuava = true
			break
		}
	}
	if !hasGuava {
		return paths
	}
	var out []string
	for _, path := range paths {
		if strings.Contains(path, string(os.PathSeparator)+"com.google.guava"+string(os.PathSeparator)+"listenablefuture"+string(os.PathSeparator)) ||
			strings.Contains(filepath.Base(path), "listenablefuture-") {
			continue
		}
		out = append(out, path)
	}
	return out
}

func artifactKeyVersion(path string) (string, string, bool) {
	marker := filepath.Join("files-2.1") + string(os.PathSeparator)
	if idx := strings.Index(path, marker); idx >= 0 {
		rest := strings.Split(path[idx+len(marker):], string(os.PathSeparator))
		if len(rest) >= 4 {
			return artifactFamilyKey(rest[0], rest[1]), rest[2], true
		}
	}
	if coord, ok := dependencywiring.CoordinateFromMaterializedPath(path); ok {
		return artifactFamilyKey(coord.Group, coord.Artifact), coord.Version, true
	}
	parts := strings.Split(filepath.Clean(path), string(os.PathSeparator))
	for i := 0; i+3 < len(parts); i++ {
		if parts[i] != "aar" {
			continue
		}
		return artifactFamilyKey(parts[i+1], parts[i+2]), parts[i+3], true
	}
	return "", "", false
}

func artifactFamilyKey(group, module string) string {
	if strings.HasPrefix(group, "androidx.") || group == "com.squareup.okhttp3" || group == "com.squareup.okio" || group == "org.jetbrains.kotlinx" {
		module = strings.TrimSuffix(module, "-jvm")
		module = strings.TrimSuffix(module, "-android")
	}
	if strings.HasPrefix(group, "androidx.compose.") {
		module = strings.TrimSuffix(module, "-desktop")
		module = strings.TrimSuffix(module, "-jvmstubs")
	}
	switch group + ":" + module {
	case "androidx.activity:activity-ktx":
		module = "activity"
	case "androidx.lifecycle:lifecycle-livedata-core-ktx":
		module = "lifecycle-livedata-core"
	case "com.sun.mail:android-activation":
		group = "javax.activation"
		module = "activation"
	case "jakarta.activation:jakarta.activation-api":
		group = "javax.activation"
		module = "activation"
	}
	if group == "asm" {
		group = "org.ow2.asm"
	}
	return group + ":" + module
}

func compareVersion(a, b string) int {
	if a == b {
		return 0
	}
	as := splitVersion(a)
	bs := splitVersion(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		ai := 0
		if i < len(as) {
			ai = as[i]
		}
		bi := 0
		if i < len(bs) {
			bi = bs[i]
		}
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	if a < b {
		return -1
	}
	return 1
}

func splitVersion(v string) []int {
	fields := strings.FieldsFunc(v, func(r rune) bool {
		return r == '.' || r == '-' || r == '[' || r == ']' || r == '(' || r == ')' || r == ','
	})
	var out []int
	for _, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil {
			out = append(out, 0)
			continue
		}
		out = append(out, n)
	}
	return out
}

func runtimeSupportJars() []string {
	candidates := []string{
		filepath.Join(os.Getenv("HOME"), "Library", "Android", "sdk", "platforms", "android-36", "android.jar"),
		filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1", "org.hamcrest", "hamcrest-core", "1.3", "42a25dc3219429f0e5d060061f71acb49bf010a0", "hamcrest-core-1.3.jar"),
	}
	candidates = append(candidates, kotlinRuntimeJars()...)
	candidates = append(candidates, kotlinTestRuntimeJars()...)
	candidates = append(candidates, kotlinTestShimJar())
	candidates = append(candidates, junitPlatformSupportJars()...)
	candidates = append(candidates, junitJupiterApiJar())
	candidates = append(candidates, junitJupiterEngineJar())
	candidates = append(candidates, junitPlatformRunnerJar())
	var out []string
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil { // #nosec
			out = append(out, candidate)
		}
	}
	return out
}

func kotlinRuntimeJars() []string {
	version := latestCachedKotlinVersion("kotlin-stdlib")
	if version == "" {
		return nil
	}
	return mergePaths(
		findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-stdlib", version),
		findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-stdlib-jdk7", version),
		findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-stdlib-jdk8", version),
	)
}

func kotlinTestRuntimeJars() []string {
	version := latestCachedKotlinVersion("kotlin-test")
	if version == "" {
		return nil
	}
	return mergePaths(
		findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-test", version),
		findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-test-junit", version),
		findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-test-junit5", version),
	)
}

func moduleOutputRelPath(modulePath string) string {
	return strings.TrimPrefix(strings.ReplaceAll(modulePath, ":", string(os.PathSeparator)), string(os.PathSeparator))
}
