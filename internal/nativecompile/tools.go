package nativecompile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kaeawc/grit/internal/proc"
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
		if toolchain != nil && len(toolchain.CompilerClasspath) > 0 {
			_, _ = fmt.Fprintln(stderr, "TRACE kotlinc compiler classpath:")
			for i, entry := range append([]string{}, toolchain.CompilerClasspath...) {
				_, _ = fmt.Fprintf(stderr, "  compilerCp[%d]=%s\n", i, entry)
			}
		}
		_, _ = fmt.Fprintln(stderr, "TRACE kotlinc classpath:")
		for i, entry := range append([]string{}, classpath...) {
			_, _ = fmt.Fprintf(stderr, "  cp[%d]=%s\n", i, entry)
		}
		if androidJar != "" {
			_, _ = fmt.Fprintf(stderr, "  androidJar=%s\n", androidJar)
		}
		_, _ = fmt.Fprintln(stderr, "TRACE kotlinc args:")
		_, _ = fmt.Fprintln(stderr, strings.Join(args, " "))
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

// defaultKSPTimeout caps how long a single KSP2 child invocation may
// run. KSP processors on large modules can legitimately need many
// minutes; the cap is meant to bound pathological live-locks (some
// processors deadlock on coroutine pools at scale) without breaking
// healthy long runs. Override via GRIT_KSP_TIMEOUT (a Go duration
// string like "30m" or "1h"); a value of 0 disables the cap entirely.
const defaultKSPTimeout = 15 * time.Minute

// kspTimeout resolves the configured per-invocation KSP timeout. The
// env var is parsed at every call so users can rerun with a longer
// budget without restarting other in-flight grit processes.
func kspTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("GRIT_KSP_TIMEOUT"))
	if raw == "" {
		return defaultKSPTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return defaultKSPTimeout
	}
	return d
}

// runKSP2 invokes the KSP2 driver as a separate JVM process. The
// runtimeJars list must contain symbol-processing-aa-embeddable plus
// its api/common-deps companions (and stdlib if not already on the
// system classpath). All processor jars are passed positionally inside
// args. KSP2 prints diagnostics to stderr; we mirror that to grit's
// stderr while also buffering for tooldiag categorization.
//
// The invocation is bounded by kspTimeout (default 15m). On timeout
// the child is SIGKILLed by exec.CommandContext and the returned
// error names the elapsed duration so the run summary makes the hang
// obvious instead of letting it look like a generic exec failure.
func runKSP2(ctx context.Context, runtimeJars []string, args []string, stdout, stderr *os.File) error {
	stdlib := kotlinRuntimeJars()
	classpath := mergePaths(runtimeJars, stdlib)
	javaArgs, err := javaMainArgs(classpath, ksp2MainClass, args)
	if err != nil {
		return err
	}
	if err := prepareJavaStartupArgs(javaArgs); err != nil {
		return err
	}
	if strings.TrimSpace(os.Getenv("GRIT_TRACE_KSP")) != "" {
		_, _ = fmt.Fprintln(stderr, "TRACE ksp2 classpath:")
		for i, entry := range classpath {
			_, _ = fmt.Fprintf(stderr, "  cp[%d]=%s\n", i, entry)
		}
		_, _ = fmt.Fprintln(stderr, "TRACE ksp2 args:")
		for _, a := range args {
			_, _ = fmt.Fprintf(stderr, "  %s\n", a)
		}
	}

	runCtx := ctx
	timeout := kspTimeout()
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, "java", javaArgs...)
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(stderr, &stderrBuf)
	startedAt := time.Now()
	err = cmd.Run()
	recordToolDiagnostics(ctx, "ksp2", stderrBuf.String(), stdoutBuf.String())
	if err != nil && timeout > 0 && errors.Is(runCtx.Err(), context.DeadlineExceeded) && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		elapsed := time.Since(startedAt).Round(time.Second)
		return fmt.Errorf("ksp2 timed out after %s (configurable via GRIT_KSP_TIMEOUT; default %s): %w", elapsed, timeout, err)
	}
	return err
}

func runJUnit(ctx context.Context, tests []string, classpath []string, stdout, stderr *os.File) error {
	classpath = junitRuntimeClasspath(classpath)
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

func runD8Command(ctx context.Context, tc *androidDexToolchain, args []string, stdout, stderr *os.File) error {
	if err := tc.validate(); err != nil {
		return err
	}
	javaArgs := append([]string{"-cp", tc.JarPath, "com.android.tools.r8.D8"}, args...)
	if err := prepareJavaStartupArgs(javaArgs); err != nil {
		return err
	}
	res, err := defaultRunner.Run(ctx, proc.Cmd{Name: "java", Args: javaArgs})
	if err == nil && res.ExitCode != 0 {
		err = fmt.Errorf("d8 exited with %d", res.ExitCode)
	}
	recordToolDiagnostics(ctx, "d8", string(res.Stderr), string(res.Stdout))
	if err != nil {
		if _, writeErr := stdout.Write(res.Stdout); writeErr != nil {
			return fmt.Errorf("d8 failed: %w (additionally failed to write stdout: %v)", err, writeErr)
		}
		if _, writeErr := stderr.Write(res.Stderr); writeErr != nil {
			return fmt.Errorf("d8 failed: %w (additionally failed to write stderr: %v)", err, writeErr)
		}
		return err
	}
	if warningLines := countNonEmptyLines(string(res.Stdout)) + countNonEmptyLines(string(res.Stderr)); warningLines > 0 {
		_, _ = fmt.Fprintf(stderr, "d8 emitted %d warning lines; suppressed after successful build\n", warningLines)
	}
	return nil
}

func runR8(ctx context.Context, tc *androidDexToolchain, mod *project.Module, variant project.BuildType, classesJar, dexDir string, runtimeCP []string, stdout, stderr *os.File) error {
	if err := tc.validate(); err != nil {
		return err
	}
	r8ProgramCP := filterR8ProgramClasspath(collapseVersions(runtimeCP))
	traceD8Inputs("r8", r8ProgramCP, stderr)
	inputs := append([]string{classesJar, androidJarPath()}, r8ProgramCP...)
	if dexOutputsFresh(dexDir, inputs, tc) {
		return nil
	}
	extraRules, err := writeGeneratedR8Rules(mod, variant)
	if err != nil {
		return err
	}
	args := r8Args(tc, androidJarPath(), mod, variant, classesJar, dexDir, r8ProgramCP, extraRules)
	if err := prepareJavaStartupArgs(args); err != nil {
		return err
	}
	res, err := defaultRunner.Run(ctx, proc.Cmd{Name: "java", Args: args})
	if err == nil && res.ExitCode != 0 {
		err = fmt.Errorf("r8 exited with %d", res.ExitCode)
	}
	recordToolDiagnostics(ctx, "r8", string(res.Stderr), string(res.Stdout))
	if err != nil {
		if _, writeErr := stdout.Write(res.Stdout); writeErr != nil {
			return fmt.Errorf("r8 failed: %w (additionally failed to write stdout: %v)", err, writeErr)
		}
		if _, writeErr := stderr.Write(res.Stderr); writeErr != nil {
			return fmt.Errorf("r8 failed: %w (additionally failed to write stderr: %v)", err, writeErr)
		}
		return err
	}
	if warningLines := countNonEmptyLines(string(res.Stdout)) + countNonEmptyLines(string(res.Stderr)); warningLines > 0 {
		_, _ = fmt.Fprintf(stderr, "r8 emitted %d warning lines; suppressed after successful build\n", warningLines)
	}
	return writeDexToolchainStamp(dexDir, tc)
}

func runCmd(ctx context.Context, bin string, args []string, stdout, stderr *os.File) error {
	return runBuffered(ctx, bin, proc.Cmd{Name: bin, Args: args}, stdout, stderr)
}

func installAPK(ctx context.Context, apkPath, deviceSerial string, stdout, stderr *os.File) error {
	args := adbInstallArgs(deviceSerial, apkPath)
	return runCmd(ctx, "adb", args, stdout, stderr)
}

func uninstallPackage(ctx context.Context, packageName, deviceSerial string, stdout, stderr *os.File) error {
	args := adbUninstallArgs(deviceSerial, packageName)
	return runCmd(ctx, "adb", args, stdout, stderr)
}
