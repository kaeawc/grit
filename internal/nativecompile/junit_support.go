package nativecompile

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	junitPlatformRunnerOnce sync.Once
	junitPlatformRunnerPath string
	junitPlatformRunnerErr  error
)

func junitPlatformRunnerJar() string {
	junitPlatformRunnerOnce.Do(func() {
		junitPlatformRunnerPath, junitPlatformRunnerErr = buildJUnitPlatformRunnerJar()
	})
	if junitPlatformRunnerErr != nil {
		return ""
	}
	return junitPlatformRunnerPath
}

func buildJUnitPlatformRunnerJar() (string, error) {
	root := filepath.Join(sharedNativeCacheRoot(), "jvm-support", "junit-platform-runner")
	jarPath := filepath.Join(root, "junit-platform-runner.jar")
	if pathIsFile(jarPath) {
		return jarPath, nil
	}
	deps := junitPlatformSupportJars()
	var classpath []string
	for _, dep := range deps {
		if dep == "" {
			continue
		}
		classpath = append(classpath, dep)
	}
	if len(classpath) == 0 {
		return "", fmt.Errorf("junit platform runtime jars not found")
	}
	srcDir := filepath.Join(root, "src")
	classesDir := filepath.Join(root, "classes")
	if err := os.MkdirAll(filepath.Join(srcDir, "grit", "junit"), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(classesDir, 0o755); err != nil {
		return "", err
	}
	sourcePath := filepath.Join(srcDir, "grit", "junit", "PlatformRunner.java")
	source := `package grit.junit;

import java.io.PrintWriter;
import java.util.ArrayList;
import java.util.List;
import org.junit.platform.engine.DiscoverySelector;
import org.junit.platform.engine.discovery.DiscoverySelectors;
import org.junit.platform.launcher.Launcher;
import org.junit.platform.launcher.core.LauncherDiscoveryRequestBuilder;
import org.junit.platform.launcher.core.LauncherFactory;
import org.junit.platform.launcher.listeners.SummaryGeneratingListener;
import org.junit.platform.launcher.listeners.TestExecutionSummary;

public final class PlatformRunner {
  public static void main(String[] args) {
    List<DiscoverySelector> selectors = new ArrayList<>();
    for (String arg : args) {
      if (arg != null && !arg.isBlank()) {
        selectors.add(DiscoverySelectors.selectClass(arg));
      }
    }
    LauncherDiscoveryRequestBuilder builder = LauncherDiscoveryRequestBuilder.request();
    builder.selectors(selectors.toArray(new DiscoverySelector[0]));
    Launcher launcher = LauncherFactory.create();
    SummaryGeneratingListener listener = new SummaryGeneratingListener();
    launcher.registerTestExecutionListeners(listener);
    launcher.execute(builder.build());
    TestExecutionSummary summary = listener.getSummary();
    if (summary.getTotalFailureCount() > 0) {
      summary.printFailuresTo(new PrintWriter(System.err, true));
      System.exit(1);
    }
  }
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		return "", err
	}
	args := []string{"-cp", strings.Join(classpath, string(os.PathListSeparator)), "-d", classesDir, sourcePath}
	cmd := exec.Command("javac", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("compile junit platform runner: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := os.RemoveAll(jarPath); err != nil {
		return "", err
	}
	cmd = exec.Command("jar", "cf", jarPath, ".")
	cmd.Dir = classesDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("package junit platform runner: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return jarPath, nil
}

func junitPlatformSupportJars() []string {
	return []string{
		findLatestCachedJar(filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1", "org.junit.platform", "junit-platform-launcher", "*", "*", "junit-platform-launcher-*.jar")),
		findLatestCachedJar(filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1", "org.junit.platform", "junit-platform-engine", "*", "*", "junit-platform-engine-*.jar")),
		findLatestCachedJar(filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1", "org.junit.platform", "junit-platform-commons", "*", "*", "junit-platform-commons-*.jar")),
		findLatestCachedJar(filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1", "org.apiguardian", "apiguardian-api", "*", "*", "apiguardian-api-*.jar")),
		findLatestCachedJar(filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1", "org.opentest4j", "opentest4j", "*", "*", "opentest4j-*.jar")),
		findLatestCachedJar(filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1", "org.jspecify", "jspecify", "*", "*", "jspecify-*.jar")),
	}
}

func junitJupiterEngineJar() string {
	return findLatestCachedJar(filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1", "org.junit.jupiter", "junit-jupiter-engine", "*", "*", "junit-jupiter-engine-*.jar"))
}

func junitJupiterApiJar() string {
	return findLatestCachedJar(filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1", "org.junit.jupiter", "junit-jupiter-api", "*", "*", "junit-jupiter-api-*.jar"))
}
