package nativecompile

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runD8(ctx context.Context, workRoot, classesJar, _ string, _ string, mergedDexDir string, runtimeCP []string, stdout, stderr *os.File) error {
	runtimeCP = collapseVersions(runtimeCP)
	traceD8Inputs("runtime", runtimeCP, stderr)
	projectLibs, externalLibs := partitionRuntimeClasspath(workRoot, runtimeCP)
	traceD8Inputs("project", projectLibs, stderr)
	traceD8Inputs("external", externalLibs, stderr)
	projectDexDir := sharedProjectDexDir(projectLibs)
	externalDexDir := sharedExternalDexDir(externalLibs)
	sharedAppDexDir := sharedAppDexDir(classesJar, runtimeCP)
	if err := runD8ForLibraries(ctx, externalDexDir, externalLibs, stdout, stderr); err != nil {
		return err
	}
	if err := runD8ForLibraries(ctx, projectDexDir, projectLibs, stdout, stderr); err != nil {
		return err
	}
	if err := runD8ForApp(ctx, classesJar, sharedAppDexDir, runtimeCP, stdout, stderr); err != nil {
		return err
	}
	mergeStampPath := filepath.Join(filepath.Dir(mergedDexDir), "dex-merge.stamp")
	mergeStampValue := dexMergeStampValue(sharedAppDexDir, projectDexDir, externalDexDir)
	if stampMatches(mergeStampPath, mergeStampValue) && hasOutputFiles(mergedDexDir) {
		return nil
	}
	if outputsNewerThanInputs(mergedDexDir, []string{sharedAppDexDir, projectDexDir, externalDexDir}) {
		_ = writeStamp(mergeStampPath, mergeStampValue)
		return nil
	}
	if err := os.RemoveAll(mergedDexDir); err != nil {
		return err
	}
	if err := os.MkdirAll(mergedDexDir, 0o755); err != nil {
		return err
	}
	if err := mergeDexDirs(mergedDexDir, sharedAppDexDir, projectDexDir, externalDexDir); err != nil {
		return err
	}
	return writeStamp(mergeStampPath, mergeStampValue)
}


func traceD8Inputs(label string, paths []string, stderr *os.File) {
	if os.Getenv("GRIT_TRACE_D8") == "" || stderr == nil {
		return
	}
	_, _ = fmt.Fprintf(stderr, "grit d8 %s inputs (%d)\n", label, len(paths))
	for _, path := range paths {
		_, _ = fmt.Fprintln(stderr, path)
	}
}

func runD8ForLibraries(ctx context.Context, dexDir string, jars []string, stdout, stderr *os.File) error {
	jars = collapseVersions(jars)
	if len(jars) == 0 {
		return os.MkdirAll(dexDir, 0o755)
	}
	if isSharedDexCacheReady(dexDir) {
		return nil
	}
	inputs := append([]string{androidJarPath()}, jars...)
	if outputsNewerThanInputs(dexDir, inputs) {
		return nil
	}
	if err := os.RemoveAll(dexDir); err != nil {
		return err
	}
	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		return err
	}
	args := []string{
		"--lib", androidJarPath(),
		"--min-api", "27",
		"--map-diagnostics", "info", "warning",
		"--output", dexDir,
	}
	args = append(args, jars...)
	return runD8Command(ctx, args, stdout, stderr)
}

func runD8ForApp(ctx context.Context, classesJar, dexDir string, runtimeCP []string, stdout, stderr *os.File) error {
	runtimeCP = collapseVersions(runtimeCP)
	if isSharedDexCacheReady(dexDir) {
		return nil
	}
	inputs := append([]string{classesJar, androidJarPath()}, runtimeCP...)
	if outputsNewerThanInputs(dexDir, inputs) {
		return nil
	}
	if err := os.RemoveAll(dexDir); err != nil {
		return err
	}
	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		return err
	}
	args := []string{
		"--lib", androidJarPath(),
		"--min-api", "27",
		"--map-diagnostics", "info", "warning",
		"--output", dexDir,
	}
	for _, jar := range runtimeCP {
		args = append(args, "--classpath", jar)
	}
	args = append(args, classesJar)
	return runD8Command(ctx, args, stdout, stderr)
}

func partitionRuntimeClasspath(workRoot string, runtimeCP []string) ([]string, []string) {
	projectRoot := filepath.Join(workRoot, "build", "grit") + string(os.PathSeparator)
	var projectLibs []string
	var externalLibs []string
	for _, path := range runtimeCP {
		clean := filepath.Clean(path)
		if strings.HasPrefix(clean, projectRoot) {
			projectLibs = append(projectLibs, path)
			continue
		}
		externalLibs = append(externalLibs, path)
	}
	return projectLibs, externalLibs
}

func addDexToAPK(ctx context.Context, apkPath, dexDir string, stdout, stderr *os.File) error {
	dexStampPath := apkPath + ".dex.stamp"
	dexStampValue := dirFingerprint(dexDir)
	if stampMatches(dexStampPath, dexStampValue) && pathIsFile(apkPath) {
		return nil
	}
	if outputsNewerThanInputs(apkPath, []string{dexDir}) {
		_ = writeStamp(dexStampPath, dexStampValue)
		return nil
	}
	dexFiles, err := filepath.Glob(filepath.Join(dexDir, "classes*.dex"))
	if err != nil {
		return err
	}
	if len(dexFiles) == 0 {
		return fmt.Errorf("no dex files produced")
	}
	archiveEntries := make([]string, 0, len(dexFiles))
	for _, dexFile := range dexFiles {
		archiveEntries = append(archiveEntries, filepath.Base(dexFile))
	}
	args := append([]string{"-q", apkPath}, archiveEntries...)
	cmd := exec.CommandContext(ctx, "zip", args...)
	cmd.Dir = dexDir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	return writeStamp(dexStampPath, dexStampValue)
}
