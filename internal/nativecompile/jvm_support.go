package nativecompile

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var (
	kotlinTestShimOnce sync.Once
	kotlinTestShimPath string
	kotlinTestShimErr  error
)

func kotlinTestShimJar() string {
	kotlinTestShimOnce.Do(func() {
		kotlinTestShimPath, kotlinTestShimErr = buildKotlinTestShimJar()
	})
	if kotlinTestShimErr != nil {
		return ""
	}
	return kotlinTestShimPath
}

func buildKotlinTestShimJar() (string, error) {
	root := filepath.Join(sharedNativeCacheRoot(), "jvm-support", "kotlin-test-shim")
	jarPath := filepath.Join(root, "kotlin-test-shim.jar")
	if pathIsFile(jarPath) {
		return jarPath, nil
	}
	commonsJar := findLatestCachedJar(filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1", "org.junit.platform", "junit-platform-commons", "*", "*", "junit-platform-commons-*.jar"))
	if commonsJar == "" {
		return "", fmt.Errorf("junit-platform-commons jar not found")
	}
	srcDir := filepath.Join(root, "src")
	classesDir := filepath.Join(root, "classes")
	if err := os.MkdirAll(filepath.Join(srcDir, "kotlin", "test"), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(classesDir, 0o755); err != nil {
		return "", err
	}
	sourcePath := filepath.Join(srcDir, "kotlin", "test", "Test.java")
	source := `package kotlin.test;

import java.lang.annotation.ElementType;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;
import org.junit.platform.commons.annotation.Testable;

@Target({ElementType.METHOD})
@Retention(RetentionPolicy.RUNTIME)
@Testable
public @interface Test {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		return "", err
	}
	cmd := exec.Command("javac", "-cp", commonsJar, "-d", classesDir, sourcePath) // #nosec
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("compile kotlin.test shim: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := os.RemoveAll(jarPath); err != nil {
		return "", err
	}
	cmd = exec.Command("jar", "cf", jarPath, ".")
	cmd.Dir = classesDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("package kotlin.test shim: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return jarPath, nil
}

func findLatestCachedJar(pattern string) string {
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}
	var filtered []string
	for _, match := range matches {
		lower := strings.ToLower(match)
		if strings.HasSuffix(lower, "-sources.jar") || strings.HasSuffix(lower, "-javadoc.jar") {
			continue
		}
		filtered = append(filtered, match)
	}
	if len(filtered) == 0 {
		filtered = matches
	}
	sort.Strings(filtered)
	return filtered[len(filtered)-1]
}
