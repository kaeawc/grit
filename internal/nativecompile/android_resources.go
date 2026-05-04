package nativecompile

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/project"
)

func runAAPT2Compile(ctx context.Context, resDir, outDir string, stdout, stderr *os.File) error {
	if _, err := os.Stat(resDir); err != nil {
		return nil
	}
	args := []string{"compile", "--dir", resDir, "-o", outDir}
	return runCmd(ctx, "aapt2", args, stdout, stderr)
}

func runAAPT2Link(ctx context.Context, prj *project.Project, mod *project.Module, variant project.BuildType, compiledFiles []string, compiledInputs []string, outAPK string, stdout, stderr *os.File) error {
	manifestPath, err := manifestForPackagingForProject(prj, mod, variant.Name)
	if err != nil {
		return err
	}
	return runAAPT2LinkWithManifest(ctx, manifestPath, mod.MinSDK, mod.TargetSDK, variant.Name == "debug", compiledFiles, compiledInputs, outAPK, stdout, stderr)
}

func runAAPT2LinkWithManifest(ctx context.Context, manifestPath, minSDK, targetSDK string, debugMode bool, compiledFiles []string, compiledInputs []string, outAPK string, stdout, stderr *os.File) error {
	androidJar := filepath.Join(os.Getenv("HOME"), "Library", "Android", "sdk", "platforms", "android-36", "android.jar")
	inputs := append([]string{manifestPath, androidJar}, compiledInputs...)
	if outputsNewerThanInputs(outAPK, inputs) {
		return nil
	}
	args := []string{
		"--manifest", manifestPath,
		"-I", androidJar,
		"--min-sdk-version", minSDK,
		"--target-sdk-version", targetSDK,
		"--auto-add-overlay",
		"-o", outAPK,
	}
	if debugMode {
		args = append(args, "--debug-mode")
	}
	linkedFiles, cleanup, err := compactAAPT2InputPaths(compiledFiles)
	if err != nil {
		return err
	}
	defer cleanup()
	for _, file := range linkedFiles {
		args = append(args, "-R", file)
	}
	traceAAPT2Args("link", args, stderr)
	return runCmd(ctx, "aapt2", append([]string{"link"}, args...), stdout, stderr)
}

func (c *Compiler) compileAndroidResources(ctx context.Context, prj *project.Project, mod *project.Module, variantName string, depResources []androidResourceArtifact, stdout, stderr *os.File) (androidResourceArtifact, error) {
	artifact := androidResourceArtifact{
		ModulePath: mod.Path,
		Namespace:  mod.Namespace,
	}
	manifestPath, err := manifestForPackagingForProject(prj, mod, variantName)
	if err != nil {
		return artifact, err
	}
	artifact.ManifestPath = manifestPath
	outRoot := filepath.Join(prj.RootDir, "build", "grit", moduleOutputRelPath(mod.Path), variantName, "resources")
	compiledDir := filepath.Join(outRoot, "compiled")
	compiledStamp := filepath.Join(outRoot, "compiled.stamp")
	resDirs := resourceRootsForVariant(mod, variantName)
	mergedResDir := filepath.Join(outRoot, "merged", "res")
	mergedResStamp := filepath.Join(outRoot, "merged-res.stamp")
	sharedCompileCacheDir := moduleResourceCompileCacheDir(mod.Path, variantName, mod.ResolveVariant(variantName).ConfigHash(), resDirs)
	if err := os.MkdirAll(compiledDir, 0o755); err != nil {
		return artifact, err
	}
	if !pathIsDir(mergedResDir) || !outputsNewerThanInputs(mergedResStamp, resDirs) {
		if err := materializeMergedResourceDir(resDirs, mergedResDir); err != nil {
			return artifact, err
		}
		if err := touchFile(mergedResStamp); err != nil {
			return artifact, err
		}
	}
	if !outputsNewerThanInputs(compiledStamp, []string{mergedResStamp}) {
		if restoreSharedResourceCompile(compiledDir, compiledStamp, sharedCompileCacheDir) {
			recordCacheProbe(c.tracker, "compileAndroidResources", true, "shared-cache-hit", "restored compiled resources from shared cache")
			artifact.CompiledStamp = compiledStamp
		} else if ensureStampFromOutput(compiledStamp, compiledDir, []string{mergedResStamp}) {
			recordCacheProbe(c.tracker, "compileAndroidResources", true, "local-up-to-date", "compiled resource outputs newer than resource inputs")
			artifact.CompiledStamp = compiledStamp
		} else {
			recordCacheProbe(c.tracker, "compileAndroidResources", false, "cache-miss", "compiled resource outputs required fresh aapt2 compile")
			if hasOutputFiles(mergedResDir) {
				if err := runAAPT2Compile(ctx, mergedResDir, compiledDir, stdout, stderr); err != nil {
					return artifact, err
				}
			}
			if err := touchFile(compiledStamp); err != nil {
				return artifact, err
			}
			_ = saveSharedResourceCompile(compiledDir, compiledStamp, sharedCompileCacheDir)
		}
	}
	compiledFiles, err := filepath.Glob(filepath.Join(compiledDir, "*.flat"))
	if err != nil {
		return artifact, err
	}
	sort.Strings(compiledFiles)
	artifact.CompiledDir = compiledDir
	artifact.CompiledFiles = compiledFiles
	if artifact.CompiledStamp == "" {
		artifact.CompiledStamp = compiledStamp
	}
	if len(compiledFiles) == 0 {
		return artifact, nil
	}

	linkOut := filepath.Join(outRoot, "symbols.apk")
	javaOut := filepath.Join(outRoot, "java")
	if err := os.MkdirAll(javaOut, 0o755); err != nil {
		return artifact, err
	}
	symbolClassesDir := filepath.Join(outRoot, "classes")
	if err := os.MkdirAll(symbolClassesDir, 0o755); err != nil {
		return artifact, err
	}
	symbolJar := filepath.Join(outRoot, "r-symbols.jar")
	sharedSymbolsCacheDir := moduleResourceSymbolsCacheDir(mod, variantName, manifestPath, depResources, artifact)
	symbolInputs := append(resourceArtifactStamps(depResources, []androidResourceArtifact{artifact}), manifestPath, androidJarPath())
	if !outputsNewerThanInputs(symbolJar, symbolInputs) {
		if !restoreSharedSymbolJar(symbolJar, sharedSymbolsCacheDir) {
			recordCacheProbe(c.tracker, "compileAndroidResources", false, "cache-miss", "resource symbol jar required fresh generation")
			if err := runAAPT2LinkForSymbols(ctx, mod, manifestPath, flattenCompiledResourceFiles(depResources, []androidResourceArtifact{artifact}), linkOut, javaOut, stdout, stderr); err != nil {
				return artifact, err
			}
			symbolSources, err := collectJavaSources(javaOut)
			if err != nil {
				return artifact, err
			}
			if len(symbolSources) == 0 {
				return artifact, nil
			}
			if err := runJavac(ctx, symbolSources, symbolClassesDir, []string{androidJarPath()}, stdout, stderr); err != nil {
				return artifact, err
			}
			if err := jarClasses(ctx, symbolClassesDir, symbolJar, stdout, stderr); err != nil {
				return artifact, err
			}
			_ = saveSharedSymbolJar(symbolJar, sharedSymbolsCacheDir)
		} else {
			recordCacheProbe(c.tracker, "compileAndroidResources", true, "shared-cache-hit", "restored resource symbol jar from shared cache")
		}
	}
	artifact.SymbolJar = symbolJar
	return artifact, nil
}

func runAAPT2LinkForSymbols(ctx context.Context, mod *project.Module, manifestPath string, compiledFiles []string, outAPK, javaOut string, stdout, stderr *os.File) error {
	args := []string{
		"--manifest", manifestPath,
		"-I", androidJarPath(),
		"--min-sdk-version", mod.MinSDK,
		"--auto-add-overlay",
		"--static-lib",
		"--non-final-ids",
		"--java", javaOut,
		"-o", outAPK,
	}
	if mod.TargetSDK != "" {
		args = append(args, "--target-sdk-version", mod.TargetSDK)
	}
	linkedFiles, cleanup, err := compactAAPT2InputPaths(compiledFiles)
	if err != nil {
		return err
	}
	defer cleanup()
	for _, file := range linkedFiles {
		args = append(args, "-R", file)
	}
	traceAAPT2Args("link", args, stderr)
	return runCmd(ctx, "aapt2", append([]string{"link"}, args...), stdout, stderr)
}

// runAAPT2LinkProto runs aapt2 link with --proto-format to produce a proto-format
// resource APK suitable for AAB assembly. The output APK contains proto-format
// AndroidManifest.xml, resources.pb, and compiled resources under res/.
func runAAPT2LinkProto(ctx context.Context, manifestPath, minSDK, targetSDK string, debugMode bool, compiledFiles []string, compiledInputs []string, outAPK string, stdout, stderr *os.File) error {
	androidJar := androidJarPath()
	inputs := append([]string{manifestPath, androidJar}, compiledInputs...)
	if outputsNewerThanInputs(outAPK, inputs) {
		return nil
	}
	args := []string{
		"--manifest", manifestPath,
		"-I", androidJar,
		"--min-sdk-version", minSDK,
		"--target-sdk-version", targetSDK,
		"--auto-add-overlay",
		"--proto-format",
		"-o", outAPK,
	}
	if debugMode {
		args = append(args, "--debug-mode")
	}
	linkedFiles, cleanup, err := compactAAPT2InputPaths(compiledFiles)
	if err != nil {
		return err
	}
	defer cleanup()
	for _, file := range linkedFiles {
		args = append(args, "-R", file)
	}
	traceAAPT2Args("link-proto", args, stderr)
	return runCmd(ctx, "aapt2", append([]string{"link"}, args...), stdout, stderr)
}

// extractProtoAPK extracts a proto-format resource APK (produced by aapt2 link
// --proto-format) into the given directory. The extracted contents include
// AndroidManifest.xml, resources.pb, and res/ entries.
func extractProtoAPK(protoAPK, destDir string) error {
	zr, err := zip.OpenReader(protoAPK)
	if err != nil {
		return fmt.Errorf("open proto APK: %w", err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(destDir, f.Name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			_ = rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		_ = rc.Close()
		_ = out.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func compactAAPT2InputPaths(compiledFiles []string) ([]string, func(), error) {
	if len(compiledFiles) == 0 || estimatedAAPT2ArgSize(compiledFiles) < 100_000 {
		return compiledFiles, func() {}, nil
	}
	stageDir, err := os.MkdirTemp("", "grit-a2-")
	if err != nil {
		return nil, nil, fmt.Errorf("create aapt2 staging dir: %w", err)
	}
	cleanup := func() {
		if strings.TrimSpace(os.Getenv("GRIT_TRACE_AAPT2")) != "" {
			return
		}
		_ = os.RemoveAll(stageDir)
	}
	out := make([]string, 0, len(compiledFiles))
	used := map[string]int{}
	for i, src := range compiledFiles {
		if _, err := os.Stat(src); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("missing compiled resource input %s: %w", src, err)
		}
		base := filepath.Base(src)
		dst := filepath.Join(stageDir, base)
		if used[base] > 0 {
			subdir := filepath.Join(stageDir, fmt.Sprintf("%04d", i))
			if err := os.MkdirAll(subdir, 0o755); err != nil {
				cleanup()
				return nil, nil, fmt.Errorf("create aapt2 staging subdir for %s: %w", src, err)
			}
			dst = filepath.Join(subdir, base)
		}
		used[base]++
		if err := os.Link(src, dst); err != nil {
			if err := copyFile(src, dst); err != nil {
				cleanup()
				return nil, nil, fmt.Errorf("stage aapt2 input %s: %w", src, err)
			}
		}
		out = append(out, dst)
	}
	return out, cleanup, nil
}

func traceAAPT2Args(subcommand string, args []string, stderr *os.File) {
	if strings.TrimSpace(os.Getenv("GRIT_TRACE_AAPT2")) == "" {
		return
	}
	_, _ = fmt.Fprintf(stderr, "TRACE aapt2 %s args:\n", subcommand)
	_, _ = fmt.Fprintln(stderr, strings.Join(append([]string{subcommand}, args...), "\n"))
}

func estimatedAAPT2ArgSize(compiledFiles []string) int {
	total := 0
	for _, file := range compiledFiles {
		total += len("-R ") + len(file) + 1
	}
	return total
}

func manifestForPackaging(mod *project.Module, variantName string) (string, error) {
	return manifestForPackagingForProject(nil, mod, variantName)
}

func manifestForPackagingForProject(prj *project.Project, mod *project.Module, variantName string) (string, error) {
	paths := manifestSourcesForVariant(mod, variantName)
	if len(paths) == 0 {
		return writeSyntheticManifest(mod)
	}
	variant := mod.ResolveVariant(variantName)
	merged, report, err := mergeManifestFiles(paths)
	if err != nil {
		return "", err
	}
	applyManifestPlaceholders(merged, manifestPlaceholders(prj, mod, variant))
	stripManifestToolsAttrs(merged)
	ensureManifestDefaults(merged, mod.Namespace)
	body, err := encodeManifestXML(merged)
	if err != nil {
		return "", err
	}
	tmp := filepath.Join(mod.Dir, "build", "grit", variantName, "AndroidManifest.xml")
	if err := writeFileIfChanged(tmp, body); err != nil {
		return "", err
	}
	if report != nil {
		if reportData, reportErr := json.MarshalIndent(report, "", "  "); reportErr == nil {
			_ = writeFileIfChanged(tmp+".merge-report.json", append(reportData, '\n'))
		}
	}
	return tmp, nil
}

func writeSyntheticManifest(mod *project.Module) (string, error) {
	if mod.Namespace == "" {
		return "", fmt.Errorf("module %s is missing AndroidManifest.xml and namespace", mod.Path)
	}
	body := fmt.Sprintf(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="%s"/>`+"\n", mod.Namespace)
	tmp := filepath.Join(mod.Dir, "build", "grit", "AndroidManifest.xml")
	if err := writeFileIfChanged(tmp, []byte(body)); err != nil {
		return "", err
	}
	return tmp, nil
}

func resourceRootsForVariant(mod *project.Module, variantName string) []string {
	if mod == nil {
		return nil
	}
	variant := mod.ResolveVariant(variantName)
	buildType := firstNonEmpty(strings.TrimSpace(variant.Coordinate.BuildType), strings.TrimSpace(variant.Config.BaseBuildType))
	var roots []string
	roots = append(roots, filepath.Join(mod.Dir, "src", "main", "res"))
	for _, flavor := range variant.Coordinate.Flavors {
		roots = append(roots, filepath.Join(mod.Dir, "src", flavor, "res"))
	}
	if buildType != "" {
		roots = append(roots, filepath.Join(mod.Dir, "src", buildType, "res"))
	}
	if variant.Name != "" {
		roots = append(roots, filepath.Join(mod.Dir, "src", variant.Name, "res"))
	}
	return uniqueOrderedPaths(roots)
}

func manifestSourcesForVariant(mod *project.Module, variantName string) []string {
	if mod == nil {
		return nil
	}
	variant := mod.ResolveVariant(variantName)
	buildType := firstNonEmpty(strings.TrimSpace(variant.Coordinate.BuildType), strings.TrimSpace(variant.Config.BaseBuildType))
	var candidates []string
	candidates = append(candidates, filepath.Join(mod.Dir, "src", "main", "AndroidManifest.xml"))
	for _, flavor := range variant.Coordinate.Flavors {
		candidates = append(candidates, filepath.Join(mod.Dir, "src", flavor, "AndroidManifest.xml"))
	}
	if buildType != "" {
		candidates = append(candidates, filepath.Join(mod.Dir, "src", buildType, "AndroidManifest.xml"))
	}
	if variant.Name != "" {
		candidates = append(candidates, filepath.Join(mod.Dir, "src", variant.Name, "AndroidManifest.xml"))
	}
	var existing []string
	for _, path := range uniqueOrderedPaths(candidates) {
		if _, err := os.Stat(path); err == nil {
			existing = append(existing, path)
		}
	}
	return existing
}

func materializeMergedResourceDir(roots []string, mergedDir string) error {
	if err := os.RemoveAll(mergedDir); err != nil {
		return err
	}
	if err := os.MkdirAll(mergedDir, 0o755); err != nil {
		return err
	}
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			target := filepath.Join(mergedDir, rel)
			if info.IsDir() {
				return os.MkdirAll(target, info.Mode())
			}
			if isValuesResourceXML(rel) && pathIsFile(target) {
				return mergeValuesResourceFiles(target, path)
			}
			return copyFile(path, target)
		}); err != nil {
			return err
		}
	}
	return nil
}

type manifestXMLNode struct {
	Name     string
	Attrs    []manifestXMLAttr
	Children []*manifestXMLNode
	Text     string
	Sources  map[string]struct{}
}

type manifestXMLAttr struct {
	Name  string
	Value string
}

type manifestMergeReport struct {
	Sources []manifestMergeSource `json:"sources,omitempty"`
	Events  []manifestMergeEvent  `json:"events,omitempty"`
}

type manifestMergeSource struct {
	Path    string `json:"path"`
	Package string `json:"package,omitempty"`
}

type manifestMergeEvent struct {
	Type          string   `json:"type"`
	Node          string   `json:"node,omitempty"`
	Directive     string   `json:"directive,omitempty"`
	SourcePackage string   `json:"sourcePackage,omitempty"`
	TargetSources []string `json:"targetSources,omitempty"`
	Message       string   `json:"message,omitempty"`
}

func mergeManifestFiles(paths []string) (*manifestXMLNode, *manifestMergeReport, error) {
	var merged *manifestXMLNode
	report := &manifestMergeReport{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		next, err := parseManifestXML(data)
		if err != nil {
			return nil, nil, fmt.Errorf("parse manifest %s: %w", path, err)
		}
		annotateManifestNodeSources(next, manifestAttr(next, "package"))
		if next == nil {
			continue
		}
		report.Sources = append(report.Sources, manifestMergeSource{Path: path, Package: manifestAttr(next, "package")})
		if merged == nil {
			merged = next
			continue
		}
		if err := mergeManifestNode(merged, next, "", manifestAttr(merged, "package"), report); err != nil {
			return nil, report, fmt.Errorf("merge manifest %s: %w", path, err)
		}
	}
	return merged, report, nil
}

func parseManifestXML(data []byte) (*manifestXMLNode, error) {
	root, err := parseManifestXMLLike(data)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("empty manifest")
	}
	if root.Name != "manifest" {
		return nil, fmt.Errorf("expected manifest root, got %s", root.Name)
	}
	return root, nil
}

func mergeManifestNode(dst, src *manifestXMLNode, parentName string, dstManifestPackage string, report *manifestMergeReport) error {
	mergeManifestNodeSources(dst, src)
	if err := mergeManifestAttrs(dst, src, dstManifestPackage, report); err != nil {
		return err
	}
	if strings.TrimSpace(src.Text) != "" {
		dst.Text = strings.TrimSpace(src.Text)
	}
	switch manifestAttr(src, "tools:node") {
	case "removeAll":
		dst.Children = filterManifestChildren(dst.Children, func(child *manifestXMLNode) bool {
			return child.Name != src.Name
		})
		return nil
	case "mergeOnlyAttributes":
		return nil
	}
	for _, child := range src.Children {
		nodeDirective := manifestAttr(child, "tools:node")
		key := manifestChildMergeKey(dst.Name, child)
		existingIndex := findManifestChildIndex(dst, key, dst.Name, child.Name)
		var targetNode *manifestXMLNode
		if existingIndex >= 0 {
			targetNode = dst.Children[existingIndex]
		}
		if !manifestSelectorMatches(child, targetNode, dstManifestPackage) {
			recordManifestMergeEvent(report, "selector-skip", child, nodeDirective, targetNode, "selector did not match target sources")
			nodeDirective = ""
		}
		if nodeDirective == "remove" {
			if existingIndex >= 0 {
				recordManifestMergeEvent(report, "directive", child, "remove", targetNode, "")
				dst.Children = append(dst.Children[:existingIndex], dst.Children[existingIndex+1:]...)
			}
			continue
		}
		if nodeDirective == "replace" {
			replacement := cloneManifestNode(child)
			stripManifestToolsAttrs(replacement)
			recordManifestMergeEvent(report, "directive", child, "replace", targetNode, "")
			if existingIndex >= 0 {
				dst.Children[existingIndex] = replacement
			} else {
				dst.Children = append(dst.Children, replacement)
			}
			continue
		}
		if nodeDirective == "mergeOnlyAttributes" {
			if existingIndex >= 0 {
				if err := mergeManifestAttrs(dst.Children[existingIndex], child, dstManifestPackage, report); err != nil {
					return err
				}
				recordManifestMergeEvent(report, "directive", child, "mergeOnlyAttributes", targetNode, "")
				if strings.TrimSpace(child.Text) != "" {
					dst.Children[existingIndex].Text = strings.TrimSpace(child.Text)
				}
				continue
			}
			appended := cloneManifestNode(child)
			appended.Children = nil
			stripManifestToolsAttrs(appended)
			dst.Children = append(dst.Children, appended)
			continue
		}
		if key == "" {
			appended := cloneManifestNode(child)
			stripManifestToolsAttrs(appended)
			dst.Children = append(dst.Children, appended)
			continue
		}
		if existing := findManifestChild(dst, key, dst.Name); existing != nil {
			if err := mergeManifestNode(existing, child, dst.Name, dstManifestPackage, report); err != nil {
				return err
			}
			continue
		}
		appended := cloneManifestNode(child)
		stripManifestToolsAttrs(appended)
		dst.Children = append(dst.Children, appended)
	}
	return nil
}

func findManifestChild(node *manifestXMLNode, key string, parentName string) *manifestXMLNode {
	for _, child := range node.Children {
		if manifestChildMergeKey(parentName, child) == key {
			return child
		}
	}
	return nil
}

func findManifestChildIndex(node *manifestXMLNode, key string, parentName string, childName string) int {
	for i, child := range node.Children {
		if key != "" && manifestChildMergeKey(parentName, child) == key {
			return i
		}
	}
	if key == "" {
		for i, child := range node.Children {
			if child.Name == childName {
				return i
			}
		}
	}
	return -1
}

func manifestChildMergeKey(parentName string, node *manifestXMLNode) string {
	if node == nil {
		return ""
	}
	switch parentName {
	case "":
		if node.Name == "manifest" {
			return "manifest"
		}
	case "manifest":
		switch node.Name {
		case "application", "uses-sdk", "queries", "supports-screens", "compatible-screens":
			return node.Name
		}
	case "application":
		switch node.Name {
		case "uses-cleartext-traffic", "profileable":
			return node.Name
		}
	}
	if name := firstNonEmpty(manifestAttr(node, "android:name"), manifestAttr(node, "name")); name != "" {
		return node.Name + "#name=" + name
	}
	if authorities := firstNonEmpty(manifestAttr(node, "android:authorities"), manifestAttr(node, "authorities")); authorities != "" {
		return node.Name + "#authorities=" + authorities
	}
	return ""
}

func cloneManifestNode(node *manifestXMLNode) *manifestXMLNode {
	if node == nil {
		return nil
	}
	out := &manifestXMLNode{
		Name:    node.Name,
		Text:    node.Text,
		Attrs:   append([]manifestXMLAttr(nil), node.Attrs...),
		Sources: cloneManifestNodeSources(node.Sources),
	}
	for _, child := range node.Children {
		out.Children = append(out.Children, cloneManifestNode(child))
	}
	return out
}

func ensureManifestDefaults(node *manifestXMLNode, namespace string) {
	if node == nil {
		return
	}
	if manifestAttr(node, "xmlns:android") == "" {
		setManifestAttr(node, "xmlns:android", "http://schemas.android.com/apk/res/android")
	}
	if manifestAttr(node, "package") == "" {
		if namespace == "" {
			return
		}
		setManifestAttr(node, "package", namespace)
	}
}

func manifestPlaceholders(prj *project.Project, mod *project.Module, variant project.ResolvedVariant) map[string]string {
	applicationID := firstNonEmpty(strings.TrimSpace(variant.ApplicationID), strings.TrimSpace(mod.ApplicationID), strings.TrimSpace(mod.Namespace))
	out := map[string]string{
		"applicationId":             applicationID,
		"packageName":               applicationID,
		"namespace":                 firstNonEmpty(strings.TrimSpace(mod.Namespace), applicationID),
		"manifestPackage":           firstNonEmpty(strings.TrimSpace(mod.Namespace), applicationID),
		"versionCode":               firstNonEmpty(strings.TrimSpace(variant.VersionCode), strings.TrimSpace(mod.VersionCode)),
		"versionName":               firstNonEmpty(strings.TrimSpace(variant.VersionName), strings.TrimSpace(mod.VersionName)),
		"minSdkVersion":             firstNonEmpty(strings.TrimSpace(variant.MinSDK), strings.TrimSpace(mod.MinSDK)),
		"targetSdkVersion":          firstNonEmpty(strings.TrimSpace(variant.TargetSDK), strings.TrimSpace(mod.TargetSDK)),
		"testInstrumentationRunner": strings.TrimSpace(mod.TestInstrumentationRunner),
	}
	if prj != nil {
		for key, value := range prj.GradleProperties {
			if strings.TrimSpace(value) == "" {
				continue
			}
			out[key] = value
			if strings.HasPrefix(key, "manifestPlaceholders.") {
				out[strings.TrimPrefix(key, "manifestPlaceholders.")] = value
			}
		}
		for key, value := range prj.VersionCatalogData {
			if strings.TrimSpace(value) == "" {
				continue
			}
			out[key] = value
			out["libs.versions."+key] = value
		}
	}
	for key, value := range moduleManifestPlaceholders(prj, mod, variant.Name) {
		if strings.TrimSpace(value) != "" {
			out[key] = strings.TrimSpace(value)
		}
	}
	for key, value := range map[string]string{
		"applicationId":             applicationID,
		"packageName":               applicationID,
		"namespace":                 firstNonEmpty(strings.TrimSpace(mod.Namespace), applicationID),
		"manifestPackage":           firstNonEmpty(strings.TrimSpace(mod.Namespace), applicationID),
		"versionCode":               firstNonEmpty(strings.TrimSpace(variant.VersionCode), strings.TrimSpace(mod.VersionCode)),
		"versionName":               firstNonEmpty(strings.TrimSpace(variant.VersionName), strings.TrimSpace(mod.VersionName)),
		"minSdkVersion":             firstNonEmpty(strings.TrimSpace(variant.MinSDK), strings.TrimSpace(mod.MinSDK)),
		"targetSdkVersion":          firstNonEmpty(strings.TrimSpace(variant.TargetSDK), strings.TrimSpace(mod.TargetSDK)),
		"testInstrumentationRunner": strings.TrimSpace(mod.TestInstrumentationRunner),
	} {
		if strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	return out
}

func moduleManifestPlaceholders(prj *project.Project, mod *project.Module, variantName string) map[string]string {
	if mod == nil || mod.BuildFile == "" {
		return nil
	}
	data, err := os.ReadFile(mod.BuildFile)
	if err != nil {
		return nil
	}
	body := string(data)
	valueVars := parseManifestValueVariables(prj, body)
	variant := mod.ResolveVariant(variantName)
	out := map[string]string{}
	if block, ok := extractNamedBlockLocal(body, "defaultConfig"); ok {
		mergeManifestPlaceholderAssignments(out, block, valueVars)
	}
	for _, flavor := range variant.Coordinate.Flavors {
		if block, ok := extractNamedBlockLocalFromBlock(body, "productFlavors", flavor); ok {
			mergeManifestPlaceholderAssignments(out, block, valueVars)
		}
	}
	if buildType := strings.TrimSpace(variant.Coordinate.BuildType); buildType != "" {
		if block, ok := extractNamedBlockLocalFromBlock(body, "buildTypes", buildType); ok {
			mergeManifestPlaceholderAssignments(out, block, valueVars)
		}
	}
	return out
}

func parseManifestValueVariables(prj *project.Project, body string) map[string]string {
	out := map[string]string{}
	envPropRe := regexp.MustCompile(`(?ms)^\s*val\s+([A-Za-z0-9_]+):\s*String\?\s*=\s*System\.getenv\("([^"]+)"\)\s*\?:\s*findProperty\("([^"]+)"\)\s+as\s+String\?`)
	for _, match := range envPropRe.FindAllStringSubmatch(body, -1) {
		if value := firstNonEmpty(os.Getenv(match[2]), gradlePropertyValue(prj, match[3])); value != "" {
			out[match[1]] = value
		}
	}
	propOnlyRe := regexp.MustCompile(`(?ms)^\s*val\s+([A-Za-z0-9_]+):\s*String\?\s*=\s*findProperty\("([^"]+)"\)\s+as\s+String\?`)
	for _, match := range propOnlyRe.FindAllStringSubmatch(body, -1) {
		if value := gradlePropertyValue(prj, match[2]); value != "" {
			out[match[1]] = value
		}
	}
	projectPropRe := regexp.MustCompile(`(?ms)^\s*val\s+([A-Za-z0-9_]+):\s*String\?\s*=\s*project\.findProperty\("([^"]+)"\)\s+as\s+String\?`)
	for _, match := range projectPropRe.FindAllStringSubmatch(body, -1) {
		if value := gradlePropertyValue(prj, match[2]); value != "" {
			out[match[1]] = value
		}
	}
	providerPropRe := regexp.MustCompile(`(?ms)^\s*val\s+([A-Za-z0-9_]+)\s*=\s*providers\.gradleProperty\("([^"]+)"\)\.orNull`)
	for _, match := range providerPropRe.FindAllStringSubmatch(body, -1) {
		if value := gradlePropertyValue(prj, match[2]); value != "" {
			out[match[1]] = value
		}
	}
	providerEnvRe := regexp.MustCompile(`(?ms)^\s*val\s+([A-Za-z0-9_]+)\s*=\s*providers\.environmentVariable\("([^"]+)"\)\.orNull`)
	for _, match := range providerEnvRe.FindAllStringSubmatch(body, -1) {
		if value := strings.TrimSpace(os.Getenv(match[2])); value != "" {
			out[match[1]] = value
		}
	}
	providerEnvOrPropRe := regexp.MustCompile(`(?ms)^\s*val\s+([A-Za-z0-9_]+)\s*=\s*providers\.environmentVariable\("([^"]+)"\)\.orElse\(providers\.gradleProperty\("([^"]+)"\)\)\.orNull`)
	for _, match := range providerEnvOrPropRe.FindAllStringSubmatch(body, -1) {
		if value := firstNonEmpty(strings.TrimSpace(os.Getenv(match[2])), gradlePropertyValue(prj, match[3])); value != "" {
			out[match[1]] = value
		}
	}
	literalRe := regexp.MustCompile(`(?m)^\s*val\s+([A-Za-z0-9_]+)\s*=\s*"([^"]+)"\s*$`)
	for _, match := range literalRe.FindAllStringSubmatch(body, -1) {
		out[match[1]] = match[2]
	}
	return out
}

func gradlePropertyValue(prj *project.Project, key string) string {
	if prj == nil {
		return ""
	}
	return strings.TrimSpace(prj.GradleProperties[key])
}

func mergeManifestPlaceholderAssignments(dst map[string]string, body string, valueVars map[string]string) {
	valueExpr := `("[^"]+"|[A-Za-z0-9_]+)`
	assignRe := regexp.MustCompile(`manifestPlaceholders\s*\[\s*"([^"]+)"\s*\]\s*=\s*` + valueExpr)
	for _, match := range assignRe.FindAllStringSubmatch(body, -1) {
		if value := resolveManifestPlaceholderValue(match[2], valueVars); value != "" {
			dst[match[1]] = value
		}
	}
	putRe := regexp.MustCompile(`manifestPlaceholders\.put\(\s*"([^"]+)"\s*,\s*` + valueExpr + `\s*\)`)
	for _, match := range putRe.FindAllStringSubmatch(body, -1) {
		if value := resolveManifestPlaceholderValue(match[2], valueVars); value != "" {
			dst[match[1]] = value
		}
	}
	mapRe := regexp.MustCompile(`manifestPlaceholders\s*\+=\s*mapOf\((?s)(.*?)\)`)
	for _, match := range mapRe.FindAllStringSubmatch(body, -1) {
		mergeManifestPlaceholderMapEntries(dst, match[1], valueVars)
	}
	assignMapRe := regexp.MustCompile(`manifestPlaceholders\s*=\s*mapOf\((?s)(.*?)\)`)
	for _, match := range assignMapRe.FindAllStringSubmatch(body, -1) {
		mergeManifestPlaceholderMapEntries(dst, match[1], valueVars)
	}
	putAllRe := regexp.MustCompile(`manifestPlaceholders\.putAll\(\s*mapOf\((?s)(.*?)\)\s*\)`)
	for _, match := range putAllRe.FindAllStringSubmatch(body, -1) {
		mergeManifestPlaceholderMapEntries(dst, match[1], valueVars)
	}
}

func mergeManifestPlaceholderMapEntries(dst map[string]string, entries string, valueVars map[string]string) {
	entryRe := regexp.MustCompile(`"([^"]+)"\s*to\s*("[^"]+"|[A-Za-z0-9_]+)`)
	for _, entry := range entryRe.FindAllStringSubmatch(entries, -1) {
		if value := resolveManifestPlaceholderValue(entry[2], valueVars); value != "" {
			dst[entry[1]] = value
		}
	}
}

func resolveManifestPlaceholderValue(expr string, valueVars map[string]string) string {
	expr = strings.TrimSpace(expr)
	if len(expr) >= 2 && strings.HasPrefix(expr, `"`) && strings.HasSuffix(expr, `"`) {
		return strings.Trim(expr, `"`)
	}
	return strings.TrimSpace(valueVars[expr])
}

func extractNamedBlockLocal(body, name string) (string, bool) {
	idx := strings.Index(body, name+" {")
	if idx < 0 {
		return "", false
	}
	openIdx := idx + len(name)
	return extractBraceBodyAtLocal(body, openIdx)
}

func extractNamedBlockLocalFromBlock(body, parent, child string) (string, bool) {
	parentBody, ok := extractNamedBlockLocal(body, parent)
	if !ok {
		return "", false
	}
	return extractNamedBlockLocal(parentBody, child)
}

func extractBraceBodyAtLocal(body string, openIdx int) (string, bool) {
	start := strings.Index(body[openIdx:], "{")
	if start < 0 {
		return "", false
	}
	start += openIdx
	depth := 0
	for i := start; i < len(body); i++ {
		switch body[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return body[start+1 : i], true
			}
		}
	}
	return "", false
}

func applyManifestPlaceholders(node *manifestXMLNode, placeholders map[string]string) {
	if node == nil {
		return
	}
	for i := range node.Attrs {
		node.Attrs[i].Value = expandPlaceholders(node.Attrs[i].Value, placeholders)
	}
	node.Text = expandPlaceholders(node.Text, placeholders)
	for _, child := range node.Children {
		applyManifestPlaceholders(child, placeholders)
	}
}

func expandPlaceholders(value string, placeholders map[string]string) string {
	out := value
	for key, replacement := range placeholders {
		if strings.TrimSpace(replacement) == "" {
			continue
		}
		out = strings.ReplaceAll(out, "${"+key+"}", replacement)
	}
	placeholderPattern := regexp.MustCompile(`\$\{([^}]+)\}`)
	out = placeholderPattern.ReplaceAllStringFunc(out, func(match string) string {
		submatches := placeholderPattern.FindStringSubmatch(match)
		if len(submatches) != 2 {
			return match
		}
		if value := strings.TrimSpace(os.Getenv(submatches[1])); value != "" {
			return value
		}
		return match
	})
	return out
}

func encodeManifestXML(node *manifestXMLNode) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeManifestNode(&buf, node, 0); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

func writeManifestNode(buf *bytes.Buffer, node *manifestXMLNode, depth int) error {
	indent := strings.Repeat("  ", depth)
	buf.WriteString(indent)
	buf.WriteByte('<')
	buf.WriteString(node.Name)
	for _, attr := range node.Attrs {
		buf.WriteByte(' ')
		buf.WriteString(attr.Name)
		buf.WriteString(`="`)
		if err := xml.EscapeText(buf, []byte(attr.Value)); err != nil {
			return err
		}
		buf.WriteByte('"')
	}
	if len(node.Children) == 0 && strings.TrimSpace(node.Text) == "" {
		buf.WriteString("/>")
		return nil
	}
	buf.WriteByte('>')
	if strings.TrimSpace(node.Text) != "" {
		if err := xml.EscapeText(buf, []byte(node.Text)); err != nil {
			return err
		}
	}
	if len(node.Children) > 0 {
		buf.WriteByte('\n')
		for i, child := range node.Children {
			if err := writeManifestNode(buf, child, depth+1); err != nil {
				return err
			}
			if i < len(node.Children) || len(node.Children) > 0 {
				buf.WriteByte('\n')
			}
		}
		buf.WriteString(indent)
	}
	buf.WriteString("</")
	buf.WriteString(node.Name)
	buf.WriteByte('>')
	return nil
}

func setManifestAttr(node *manifestXMLNode, name, value string) {
	for i := range node.Attrs {
		if node.Attrs[i].Name == name {
			node.Attrs[i].Value = value
			return
		}
	}
	node.Attrs = append(node.Attrs, manifestXMLAttr{Name: name, Value: value})
}

func mergeManifestAttrs(dst, src *manifestXMLNode, dstManifestPackage string, report *manifestMergeReport) error {
	if dst == nil || src == nil {
		return nil
	}
	replace := manifestDirectiveSet(src, "tools:replace", dst, dstManifestPackage)
	remove := manifestDirectiveSet(src, "tools:remove", dst, dstManifestPackage)
	strict := manifestDirectiveSet(src, "tools:strict", dst, dstManifestPackage)
	for _, attr := range src.Attrs {
		if strings.HasPrefix(attr.Name, "tools:") || attr.Name == "xmlns:tools" {
			continue
		}
		if _, drop := remove[attr.Name]; drop {
			continue
		}
		if manifestAttr(dst, attr.Name) == "" {
			setManifestAttr(dst, attr.Name, attr.Value)
			continue
		}
		if _, requireMatch := strict[attr.Name]; requireMatch && manifestAttr(dst, attr.Name) != attr.Value {
			recordManifestMergeEvent(report, "strict-conflict", src, "strict", dst, fmt.Sprintf("%s existing=%q incoming=%q", attr.Name, manifestAttr(dst, attr.Name), attr.Value))
			return fmt.Errorf("tools:strict conflict on %s for %s: existing=%q incoming=%q", attr.Name, dst.Name, manifestAttr(dst, attr.Name), attr.Value)
		}
		if _, force := replace[attr.Name]; force || manifestAttr(dst, attr.Name) != attr.Value {
			setManifestAttr(dst, attr.Name, attr.Value)
		}
	}
	for attrName := range remove {
		removeManifestAttr(dst, attrName)
	}
	return nil
}

func removeManifestAttr(node *manifestXMLNode, name string) {
	if node == nil {
		return
	}
	filtered := node.Attrs[:0]
	for _, attr := range node.Attrs {
		if attr.Name == name {
			continue
		}
		filtered = append(filtered, attr)
	}
	node.Attrs = filtered
}

func stripManifestToolsAttrs(node *manifestXMLNode) {
	if node == nil {
		return
	}
	filtered := node.Attrs[:0]
	for _, attr := range node.Attrs {
		if strings.HasPrefix(attr.Name, "tools:") || attr.Name == "xmlns:tools" {
			continue
		}
		filtered = append(filtered, attr)
	}
	node.Attrs = filtered
	for _, child := range node.Children {
		stripManifestToolsAttrs(child)
	}
}

func parseToolsAttrList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func manifestDirectiveSet(node *manifestXMLNode, attrName string, targetNode *manifestXMLNode, dstManifestPackage string) map[string]struct{} {
	out := map[string]struct{}{}
	if node == nil {
		return out
	}
	if !manifestSelectorMatches(node, targetNode, dstManifestPackage) {
		return out
	}
	for _, name := range parseToolsAttrList(manifestAttr(node, attrName)) {
		out[name] = struct{}{}
	}
	return out
}

func recordManifestMergeEvent(report *manifestMergeReport, eventType string, sourceNode *manifestXMLNode, directive string, targetNode *manifestXMLNode, message string) {
	if report == nil {
		return
	}
	report.Events = append(report.Events, manifestMergeEvent{
		Type:          eventType,
		Node:          firstNonEmpty(strings.TrimSpace(sourceNode.Name), strings.TrimSpace(directive)),
		Directive:     directive,
		SourcePackage: manifestPrimarySource(sourceNode),
		TargetSources: manifestSourceList(targetNode),
		Message:       strings.TrimSpace(message),
	})
}

func manifestSelectorMatches(node *manifestXMLNode, targetNode *manifestXMLNode, dstManifestPackage string) bool {
	selector := strings.TrimSpace(manifestAttr(node, "tools:selector"))
	if selector == "" {
		return true
	}
	if targetNode != nil {
		if _, ok := targetNode.Sources[selector]; ok {
			return true
		}
	}
	return selector == strings.TrimSpace(dstManifestPackage)
}

func annotateManifestNodeSources(node *manifestXMLNode, sourcePackage string) {
	if node == nil || strings.TrimSpace(sourcePackage) == "" {
		return
	}
	if node.Sources == nil {
		node.Sources = map[string]struct{}{}
	}
	node.Sources[sourcePackage] = struct{}{}
	for _, child := range node.Children {
		annotateManifestNodeSources(child, sourcePackage)
	}
}

func mergeManifestNodeSources(dst, src *manifestXMLNode) {
	if dst == nil || src == nil {
		return
	}
	if dst.Sources == nil {
		dst.Sources = map[string]struct{}{}
	}
	for source := range src.Sources {
		dst.Sources[source] = struct{}{}
	}
}

func cloneManifestNodeSources(in map[string]struct{}) map[string]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(in))
	for key := range in {
		out[key] = struct{}{}
	}
	return out
}

func manifestPrimarySource(node *manifestXMLNode) string {
	for _, source := range manifestSourceList(node) {
		return source
	}
	return ""
}

func manifestSourceList(node *manifestXMLNode) []string {
	if node == nil || len(node.Sources) == 0 {
		return nil
	}
	var out []string
	for source := range node.Sources {
		out = append(out, source)
	}
	sort.Strings(out)
	return out
}

func filterManifestChildren(children []*manifestXMLNode, keep func(*manifestXMLNode) bool) []*manifestXMLNode {
	var out []*manifestXMLNode
	for _, child := range children {
		if keep(child) {
			out = append(out, child)
		}
	}
	return out
}

func isValuesResourceXML(rel string) bool {
	rel = filepath.ToSlash(rel)
	dir := filepath.Dir(rel)
	return strings.HasPrefix(dir, "values") && strings.HasSuffix(rel, ".xml")
}

func mergeValuesResourceFiles(dstPath, srcPath string) error {
	dstData, err := os.ReadFile(dstPath)
	if err != nil {
		return err
	}
	srcData, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	dstNode, err := parseValuesResourcesXML(dstData)
	if err != nil {
		return copyFile(srcPath, dstPath)
	}
	srcNode, err := parseValuesResourcesXML(srcData)
	if err != nil {
		return copyFile(srcPath, dstPath)
	}
	mergeValuesNodes(dstNode, srcNode)
	data, err := encodeManifestXML(dstNode)
	if err != nil {
		return err
	}
	return writeFileIfChanged(dstPath, data)
}

func parseValuesResourcesXML(data []byte) (*manifestXMLNode, error) {
	root, err := parseManifestXMLLike(data)
	if err != nil {
		return nil, err
	}
	if root == nil || root.Name != "resources" {
		return nil, fmt.Errorf("expected resources root")
	}
	return root, nil
}

func parseManifestXMLLike(data []byte) (*manifestXMLNode, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var stack []*manifestXMLNode
	var root *manifestXMLNode
	for {
		token, err := decoder.RawToken()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		switch tok := token.(type) {
		case xml.StartElement:
			node := &manifestXMLNode{Name: qualifiedXMLName(tok.Name)}
			for _, attr := range tok.Attr {
				node.Attrs = append(node.Attrs, manifestXMLAttr{Name: qualifiedXMLName(attr.Name), Value: attr.Value})
			}
			if len(stack) == 0 {
				root = node
			} else {
				stack[len(stack)-1].Children = append(stack[len(stack)-1].Children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("unexpected closing tag %s", qualifiedXMLName(tok.Name))
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				continue
			}
			text := strings.TrimSpace(string(tok))
			if text == "" {
				continue
			}
			current := stack[len(stack)-1]
			if current.Text == "" {
				current.Text = text
			} else {
				current.Text += text
			}
		}
	}
	return root, nil
}

func mergeValuesNodes(dst, src *manifestXMLNode) {
	for _, attr := range src.Attrs {
		setManifestAttr(dst, attr.Name, attr.Value)
	}
	for _, child := range src.Children {
		key := valuesResourceKey(child)
		if key == "" {
			dst.Children = append(dst.Children, cloneManifestNode(child))
			continue
		}
		index := -1
		for i, existing := range dst.Children {
			if valuesResourceKey(existing) == key {
				index = i
				break
			}
		}
		if index >= 0 {
			dst.Children[index] = cloneManifestNode(child)
			continue
		}
		dst.Children = append(dst.Children, cloneManifestNode(child))
	}
}

func valuesResourceKey(node *manifestXMLNode) string {
	if node == nil {
		return ""
	}
	name := manifestAttr(node, "name")
	if node.Name == "item" {
		return node.Name + ":" + manifestAttr(node, "type") + ":" + name
	}
	if name != "" {
		return node.Name + ":" + name
	}
	return ""
}

func manifestAttr(node *manifestXMLNode, name string) string {
	for _, attr := range node.Attrs {
		if attr.Name == name {
			return attr.Value
		}
	}
	return ""
}

func qualifiedXMLName(name xml.Name) string {
	if strings.TrimSpace(name.Space) == "" {
		return name.Local
	}
	return name.Space + ":" + name.Local
}
