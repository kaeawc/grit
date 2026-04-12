package nativecompile

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kaeawc/grit/internal/project"
)

func runKotlinc(ctx context.Context, toolchain *kotlinToolchain, sources []string, outDir string, classpath []string, plugins []string, pluginOptions []string, includeAndroidJar bool, appMain bool, extraArgs []string, stdout, stderr *os.File) error {
	androidJar := ""
	if includeAndroidJar {
		androidJar = androidJarPath()
	}
	classpath = compilerRuntimeClasspath(toolchain, classpath)
	args := kotlincArgs(androidJar, sources, outDir, classpath, plugins, pluginOptions, appMain, extraArgs)
	if strings.TrimSpace(os.Getenv("GRIT_TRACE_KOTLINC")) != "" {
		fmt.Fprintln(stderr, "TRACE kotlinc classpath:")
		for i, entry := range append([]string{}, classpath...) {
			fmt.Fprintf(stderr, "  cp[%d]=%s\n", i, entry)
		}
		if androidJar != "" {
			fmt.Fprintf(stderr, "  androidJar=%s\n", androidJar)
		}
		fmt.Fprintln(stderr, "TRACE kotlinc args:")
		fmt.Fprintln(stderr, strings.Join(args, " "))
	}
	var cmd *exec.Cmd
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	if toolchain != nil && len(toolchain.CompilerClasspath) > 0 {
		if err := toolchain.validate(); err != nil {
			return err
		}
		javaArgs, err := javaMainArgs(toolchain.CompilerClasspath, "org.jetbrains.kotlin.cli.jvm.K2JVMCompiler", args)
		if err != nil {
			return err
		}
		if err := prepareJavaStartupArgs(javaArgs); err != nil {
			return err
		}
		cmd = exec.CommandContext(ctx, "java", javaArgs...)
	} else {
		cmd = exec.CommandContext(ctx, "kotlinc", args...)
	}
	cmd.Stdout = io.MultiWriter(stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(stderr, &stderrBuf)
	err := cmd.Run()
	recordToolDiagnostics(ctx, "kotlinc", stderrBuf.String(), stdoutBuf.String())
	return err
}

func runJUnit(ctx context.Context, tests []string, classpath []string, stdout, stderr *os.File) error {
	classpath = mergePaths(classpath, runtimeSupportJars())
	return runJavaMain(ctx, classpath, "grit.junit.PlatformRunner", tests, stdout, stderr)
}

func runJavac(ctx context.Context, sources []string, outDir string, classpath []string, stdout, stderr *os.File) error {
	args := javacArgs(sources, outDir, classpath)
	cmd := exec.CommandContext(ctx, "javac", args...)
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(stderr, &stderrBuf)
	err := cmd.Run()
	recordToolDiagnostics(ctx, "javac", stderrBuf.String(), stdoutBuf.String())
	return err
}

func jarClasses(ctx context.Context, classesDir, outJar string, stdout, stderr *os.File) error {
	if err := os.RemoveAll(outJar); err != nil {
		return err
	}
	args := jarArgs(outJar)
	cmd := exec.CommandContext(ctx, "jar", args...)
	cmd.Dir = classesDir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func classesJarForDir(ctx context.Context, classesDir string, stdout, stderr *os.File) (string, error) {
	outJar := filepath.Join(filepath.Dir(classesDir), "module-classes.jar")
	jarStampPath := outJar + ".stamp"
	jarStampValue := classesJarStampValue(classesDir)
	if stampMatches(jarStampPath, jarStampValue) && pathIsFile(outJar) {
		return outJar, nil
	}
	if outputsNewerThanInputs(outJar, []string{classesDir}) {
		_ = writeStamp(jarStampPath, jarStampValue)
		return outJar, nil
	}
	if err := jarClasses(ctx, classesDir, outJar, stdout, stderr); err != nil {
		return "", err
	}
	_ = writeStamp(jarStampPath, jarStampValue)
	return outJar, nil
}

func runD8Command(ctx context.Context, args []string, stdout, stderr *os.File) error {
	cmd := exec.CommandContext(ctx, "d8", args...)
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	recordToolDiagnostics(ctx, "d8", stderrBuf.String(), stdoutBuf.String())
	if err != nil {
		if _, writeErr := stdout.Write(stdoutBuf.Bytes()); writeErr != nil {
			return fmt.Errorf("d8 failed: %w (additionally failed to write stdout: %v)", err, writeErr)
		}
		if _, writeErr := stderr.Write(stderrBuf.Bytes()); writeErr != nil {
			return fmt.Errorf("d8 failed: %w (additionally failed to write stderr: %v)", err, writeErr)
		}
		return err
	}
	if warningLines := countNonEmptyLines(stdoutBuf.String()) + countNonEmptyLines(stderrBuf.String()); warningLines > 0 {
		fmt.Fprintf(stderr, "d8 emitted %d warning lines; suppressed after successful build\n", warningLines)
	}
	return nil
}

func runR8(ctx context.Context, mod *project.Module, variant project.BuildType, classesJar, dexDir string, runtimeCP []string, stdout, stderr *os.File) error {
	inputs := append([]string{classesJar, androidJarPath()}, collapseVersions(runtimeCP)...)
	if outputsNewerThanInputs(dexDir, inputs) {
		return nil
	}
	extraRules, err := writeGeneratedR8Rules(mod, variant)
	if err != nil {
		return err
	}
	args := r8Args(androidJarPath(), mod, variant, classesJar, dexDir, collapseVersions(runtimeCP), extraRules)
	if err := prepareJavaStartupArgs(args); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "java", args...)
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err = cmd.Run()
	recordToolDiagnostics(ctx, "r8", stderrBuf.String(), stdoutBuf.String())
	if err != nil {
		if _, writeErr := stdout.Write(stdoutBuf.Bytes()); writeErr != nil {
			return fmt.Errorf("r8 failed: %w (additionally failed to write stdout: %v)", err, writeErr)
		}
		if _, writeErr := stderr.Write(stderrBuf.Bytes()); writeErr != nil {
			return fmt.Errorf("r8 failed: %w (additionally failed to write stderr: %v)", err, writeErr)
		}
		return err
	}
	if warningLines := countNonEmptyLines(stdoutBuf.String()) + countNonEmptyLines(stderrBuf.String()); warningLines > 0 {
		fmt.Fprintf(stderr, "r8 emitted %d warning lines; suppressed after successful build\n", warningLines)
	}
	return nil
}

func runCmd(ctx context.Context, bin string, args []string, stdout, stderr *os.File) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(stderr, &stderrBuf)
	err := cmd.Run()
	recordToolDiagnostics(ctx, bin, stderrBuf.String(), stdoutBuf.String())
	return err
}

func installAPK(ctx context.Context, apkPath, deviceSerial string, stdout, stderr *os.File) error {
	args := adbInstallArgs(deviceSerial, apkPath)
	return runCmd(ctx, "adb", args, stdout, stderr)
}

func uninstallPackage(ctx context.Context, packageName, deviceSerial string, stdout, stderr *os.File) error {
	args := adbUninstallArgs(deviceSerial, packageName)
	return runCmd(ctx, "adb", args, stdout, stderr)
}
