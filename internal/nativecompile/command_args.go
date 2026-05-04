package nativecompile

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kaeawc/grit/internal/project"
)

func kotlincArgs(androidJar string, sources []string, outDir string, classpath []string, plugins []string, pluginOptions []string, appMain bool, extraArgs []string) []string {
	args := []string{
		"-jvm-target", "21",
		"-no-stdlib",
		"-no-reflect",
		"-Xcontext-parameters",
		"-d", outDir,
	}
	if appMain {
		args = append(args,
			"-opt-in=kotlin.time.ExperimentalTime",
			"-opt-in=kotlin.RequiresOptIn",
			"-opt-in=kotlinx.coroutines.ExperimentalCoroutinesApi",
			"-opt-in=kotlin.ExperimentalUnsignedTypes",
			"-opt-in=kotlinx.coroutines.FlowPreview",
		)
	}
	for _, plugin := range plugins {
		args = append(args, "-Xplugin="+plugin)
	}
	for _, option := range pluginOptions {
		args = append(args, "-P", option)
	}
	args = append(args, extraArgs...)
	fullCP := append([]string{}, classpath...)
	if strings.TrimSpace(androidJar) != "" {
		fullCP = append([]string{androidJar}, fullCP...)
	}
	args = append(args, "-classpath", strings.Join(fullCP, string(os.PathListSeparator)))
	args = append(args, sources...)
	return args
}

func junitPlatformArgs(tests []string, classpath []string) []string {
	args := []string{
		"-cp", strings.Join(classpath, string(os.PathListSeparator)),
		"grit.junit.PlatformRunner",
	}
	return append(args, tests...)
}

func javacArgs(sources []string, outDir string, classpath []string) []string {
	args := []string{"-source", "21", "-target", "21", "-d", outDir}
	if len(classpath) > 0 {
		args = append(args, "-classpath", strings.Join(classpath, string(os.PathListSeparator)))
	}
	return append(args, sources...)
}

func jarArgs(outJar string) []string {
	return []string{"cf", outJar, "."}
}

func d8LibraryArgs(androidJar string, jars []string, dexDir string) []string {
	args := []string{
		"--lib", androidJar,
		"--min-api", "27",
		"--map-diagnostics", "info", "warning",
		"--output", dexDir,
	}
	return append(args, jars...)
}

func d8AppArgs(androidJar string, classesJar string, runtimeCP []string, dexDir string) []string {
	args := []string{
		"--lib", androidJar,
		"--min-api", "27",
		"--map-diagnostics", "info", "warning",
		"--output", dexDir,
	}
	for _, jar := range runtimeCP {
		args = append(args, "--classpath", jar)
	}
	return append(args, classesJar)
}

func d8ReleaseLibraryArgs(androidJar string, minAPI string, jars []string, dexDir string) []string {
	args := []string{
		"--release",
		"--lib", androidJar,
		"--min-api", minAPI,
		"--map-diagnostics", "info", "warning",
		"--output", dexDir,
	}
	return append(args, jars...)
}

func d8ReleaseAppArgs(androidJar string, minAPI string, classesJar string, runtimeCP []string, dexDir string) []string {
	args := []string{
		"--release",
		"--lib", androidJar,
		"--min-api", minAPI,
		"--map-diagnostics", "info", "warning",
		"--output", dexDir,
	}
	for _, jar := range runtimeCP {
		args = append(args, "--classpath", jar)
	}
	return append(args, classesJar)
}

func r8Args(androidJar string, mod *project.Module, variant project.BuildType, classesJar, dexDir string, runtimeCP []string, extraRules string) []string {
	args := []string{
		"-cp", filepath.Join(os.Getenv("HOME"), "Library", "Android", "sdk", "build-tools", "36.0.0", "lib", "d8.jar"),
		"com.android.tools.r8.R8",
		"--release",
		"--lib", androidJar,
		"--min-api", mod.MinSDK,
		"--map-diagnostics", "info", "warning",
		"--output", dexDir,
		"--pg-compat",
	}
	for _, pg := range variant.ProguardFiles {
		args = append(args, "--pg-conf", pg)
	}
	if extraRules != "" {
		args = append(args, "--pg-conf", extraRules)
	}
	args = append(args, runtimeCP...)
	return append(args, classesJar)
}

func adbInstallArgs(deviceSerial, apkPath string) []string {
	args := []string{}
	if strings.TrimSpace(deviceSerial) != "" {
		args = append(args, "-s", deviceSerial)
	}
	return append(args, "install", "-r", apkPath)
}

func adbUninstallArgs(deviceSerial, packageName string) []string {
	args := []string{}
	if strings.TrimSpace(deviceSerial) != "" {
		args = append(args, "-s", deviceSerial)
	}
	return append(args, "uninstall", packageName)
}
