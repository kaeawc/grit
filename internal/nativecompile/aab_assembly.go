package nativecompile

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// bundletoolBuildBundleArgs returns the java command arguments to invoke
// bundletool's build-bundle command. moduleZips is the list of module zip
// files (e.g. base.zip), outputAAB is the target .aab path, and
// bundleConfigPath is the optional BundleConfig.pb path (empty to omit).
func bundletoolBuildBundleArgs(jarPath string, moduleZips []string, outputAAB string, bundleConfigPath string) []string {
	args := []string{
		"-jar", jarPath,
		"build-bundle",
		"--modules=" + strings.Join(moduleZips, ","),
		"--output=" + outputAAB,
	}
	if bundleConfigPath != "" {
		args = append(args, "--config="+bundleConfigPath)
	}
	return args
}

// runBundletoolBuildBundle invokes bundletool's build-bundle command to
// produce an AAB from the given module zip files. If bundleConfigPath is
// non-empty it is passed as the --config flag.
func runBundletoolBuildBundle(ctx context.Context, tc *bundletoolToolchain, moduleZips []string, outputAAB string, bundleConfigPath string, stdout, stderr *os.File) error {
	if err := tc.validate(); err != nil {
		return fmt.Errorf("bundletool: %w", err)
	}
	if len(moduleZips) == 0 {
		return fmt.Errorf("bundletool build-bundle: no module zips provided")
	}
	for _, z := range moduleZips {
		if !pathIsFile(z) {
			return fmt.Errorf("bundletool build-bundle: module zip not found: %s", z)
		}
	}

	args := bundletoolBuildBundleArgs(tc.JarPath, moduleZips, outputAAB, bundleConfigPath)
	cmd := exec.CommandContext(ctx, "java", args...)
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(stderr, &stderrBuf)
	err := cmd.Run()
	recordToolDiagnostics(ctx, "bundletool", stderrBuf.String(), stdoutBuf.String())
	if err != nil {
		return fmt.Errorf("bundletool build-bundle failed: %w", err)
	}
	return nil
}
