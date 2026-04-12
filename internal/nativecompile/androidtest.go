package nativecompile

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kaeawc/grit/internal/dependencywiring"
	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

type androidTestCompileOutputs struct {
	variantName      string
	testClassesDir   string
	runtimeClasspath []string
}

func (c *Compiler) compileAndroidTest(ctx context.Context, prj *project.Project, mod *project.Module, variantName string, stdout, stderr *os.File) error {
	outputs, err := c.compileAndroidTestOutputs(ctx, prj, mod, variantName, stdout, stderr)
	if err != nil {
		return err
	}
	if outputs.testClassesDir == "" {
		fmt.Fprintln(stdout, "no androidTest sources found")
		return nil
	}
	fmt.Fprintln(stdout, "androidTest sources compiled")
	return nil
}

func (c *Compiler) compileAndroidTestOutputs(ctx context.Context, prj *project.Project, mod *project.Module, variantName string, stdout, stderr *os.File) (androidTestCompileOutputs, error) {
	if variantName == "" {
		variantName = mod.DefaultVariantName()
	}
	outputs := androidTestCompileOutputs{variantName: variantName}
	state := newCompileState()
	toolchain, err := state.kotlinToolchainForProject(prj)
	if err != nil {
		return outputs, err
	}
	var mainOut string
	var cp []string
	err = c.track("compileMain", func() error {
		var innerErr error
		mainOut, cp, _, innerErr = c.compileMainInternal(ctx, prj, mod, variantName, state, nil, stdout, stderr)
		return innerErr
	})
	if err != nil {
		return outputs, err
	}

	var testSources []string
	err = c.track("collectAndroidTestSources", func() error {
		var innerErr error
		testSources, innerErr = collectAndroidTestSources(mod, variantName)
		return innerErr
	})
	if err != nil {
		return outputs, err
	}
	if len(testSources) == 0 {
		return outputs, nil
	}

	var deps *modulebuild.Dependencies
	err = c.track("parseDependencies", func() error {
		var innerErr error
		deps, innerErr = modulebuild.ParseDependencies(mod.BuildFile)
		if innerErr == nil {
			deps = dependenciesForVariant(deps, mod, variantName)
		}
		return innerErr
	})
	if err != nil {
		return outputs, err
	}

	var resolver dependencywiring.DependencyResolver
	err = c.track("loadCatalog", func() error {
		var innerErr error
		resolver, innerErr = state.resolverForProject(prj)
		return innerErr
	})
	if err != nil {
		return outputs, err
	}
	resolver.SetTracker(c.tracker)
	compileDeps := modulebuild.Dependencies{
		Test: append(append([]modulebuild.Ref{}, deps.AndroidTest...), deps.AndroidTestCompileOnly...),
	}
	var resolved *m2local.Resolved
	err = c.track("resolveAndroidTestDependencies", func() error {
		var innerErr error
		resolved, innerErr = resolver.Resolve(&compileDeps)
		return innerErr
	})
	if err != nil {
		return outputs, err
	}
	err = c.track("mergeLocalAndroidTestDependencyRefs", func() error {
		localRefs := resolveLocalDependencyRefs(prj, mod, append(append([]modulebuild.Ref{}, deps.AndroidTest...), deps.AndroidTestCompileOnly...))
		resolved.TestJars = mergePaths(resolved.TestJars, localRefs)
		return nil
	})
	if err != nil {
		return outputs, err
	}

	var projectTestCP []string
	err = c.track("resolveProjectAndroidTestDeps", func() error {
		compileRefs := append(append([]modulebuild.Ref{}, deps.AndroidTest...), deps.AndroidTestCompileOnly...)
		runtimeRefs := append(append([]modulebuild.Ref{}, deps.AndroidTest...), deps.AndroidTestRuntimeOnly...)
		var innerErr error
		projectTestCP, _, _, innerErr = c.resolveProjectDeps(ctx, prj, mod.Path+"#androidTest", compileRefs, runtimeRefs, variantName, state, nil, stdout, stderr)
		return innerErr
	})
	if err != nil {
		return outputs, err
	}

	testOut := filepath.Join(prj.RootDir, "build", "grit", moduleOutputRelPath(mod.Path), variantName+"AndroidTest", "classes")
	if err := os.MkdirAll(testOut, 0o755); err != nil {
		return outputs, err
	}
	testCP := append([]string{}, resolved.TestJars...)
	testCP = append(testCP, toolchain.TestRuntimeJars...)
	testCP = append(testCP, projectTestCP...)
	testCP = append(testCP, mainOut)
	testCP = append(testCP, cp...)
	testCompileInputs := append([]string{}, testSources...)
	testCompileInputs = append(testCompileInputs, mod.BuildFile)
	testCompileInputs = append(testCompileInputs, prj.VersionCatalogs...)
	testCompileInputs = append(testCompileInputs, mainOut)
	testCompileInputs = append(testCompileInputs, testCP...)
	testCompileInputs = append(testCompileInputs, toolchain.RuntimeJars...)
	testCompileInputs = append(testCompileInputs, toolchain.CompilerClasspath...)
	testSharedCompileDir := moduleCompileCacheDir(mod.Path+"#androidTest", variantName, mod.ResolveVariant(variantName).ConfigHash(), testCompileInputs)
	testJarPath := filepath.Join(filepath.Dir(testOut), "android-test-classes.jar")
	testCompileStampPath := filepath.Join(filepath.Dir(testOut), "compile.stamp")
	endCompileTests := c.beginSerial("compileAndroidTests")
	err = c.track("restoreCompileAndroidTestsCache", func() error {
		if stampMatches(testCompileStampPath, testSharedCompileDir) && hasOutputFiles(testOut) {
			recordCacheProbe(c.tracker, "compileAndroidTests", true, "local-up-to-date", "androidTest compile stamp matched local outputs")
			return nil
		}
		if outputsNewerThanInputs(testOut, testCompileInputs) {
			_ = writeStamp(testCompileStampPath, testSharedCompileDir)
			recordCacheProbe(c.tracker, "compileAndroidTests", true, "local-up-to-date", "compiled androidTest outputs newer than inputs")
			return nil
		}
		if restoreSharedCompileCache(testOut, testJarPath, testSharedCompileDir) {
			_ = writeStamp(testCompileStampPath, testSharedCompileDir)
			recordCacheProbe(c.tracker, "compileAndroidTests", true, "shared-cache-hit", "restored compiled androidTests from shared cache")
		}
		return nil
	})
	if err == nil && !stampMatches(testCompileStampPath, testSharedCompileDir) {
		recordCacheProbe(c.tracker, "compileAndroidTests", false, "cache-miss", "compiled androidTest sources required fresh Kotlin compilation")
		err = c.track("kotlincAndroidTests", func() error {
			return runKotlinc(ctx, toolchain, testSources, testOut, testCP, nil, nil, true, false, []string{"-Xfriend-paths=" + mainOut}, stdout, stderr)
		})
		if err == nil {
			err = c.track("publishCompileAndroidTestsCache", func() error {
				testJar, innerErr := classesJarForDir(ctx, testOut, stdout, stderr)
				if innerErr != nil {
					return innerErr
				}
				testJarPath = testJar
				if innerErr := saveSharedCompileCache(testOut, testJar, testSharedCompileDir); innerErr != nil {
					return innerErr
				}
				return writeStamp(testCompileStampPath, testSharedCompileDir)
			})
		}
	}
	endCompileTests()
	if err != nil {
		return outputs, err
	}
	outputs.testClassesDir = testOut
	outputs.runtimeClasspath = mergePaths(testCP, toolchain.RuntimeJars)
	return outputs, nil
}

func (c *Compiler) InstallAndroidTestVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, deviceSerial string, stdout, stderr *os.File) error {
	mod := prj.FindModule(modulePath)
	if mod == nil {
		return fmt.Errorf("module %s not found", modulePath)
	}
	if err := c.InstallVariant(ctx, prj, modulePath, variantName, deviceSerial, stdout, stderr); err != nil {
		return err
	}
	apkPath, err := c.assembleAndroidTestAPK(ctx, prj, mod, variantName, stdout, stderr)
	if err != nil || apkPath == "" {
		return err
	}
	if err := c.track("installAndroidTestAPK", func() error {
		return installAPK(ctx, apkPath, deviceSerial, stdout, stderr)
	}); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "installed %s androidTest APK: %s\n", variantName, apkPath)
	return nil
}

func (c *Compiler) UninstallAndroidTestVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, deviceSerial string, stdout, stderr *os.File) error {
	mod := prj.FindModule(modulePath)
	if mod == nil {
		return fmt.Errorf("module %s not found", modulePath)
	}
	packageName := androidTestApplicationID(mod, variantName)
	if packageName == "" {
		return fmt.Errorf("module %s does not declare an androidTest package name", mod.Path)
	}
	return c.track("uninstallAndroidTestAPK", func() error {
		return uninstallPackage(ctx, packageName, deviceSerial, stdout, stderr)
	})
}

func (c *Compiler) assembleAndroidTestAPK(ctx context.Context, prj *project.Project, mod *project.Module, variantName string, stdout, stderr *os.File) (string, error) {
	outputs, err := c.compileAndroidTestOutputs(ctx, prj, mod, variantName, stdout, stderr)
	if err != nil || outputs.testClassesDir == "" {
		return "", err
	}
	variant := mod.Variant(outputs.variantName)
	manifestPath, err := androidTestManifestForPackaging(prj, mod, outputs.variantName)
	if err != nil {
		return "", err
	}
	var apkPath string
	err = c.track("assembleAndroidTestAPK", func() error {
		var innerErr error
		apkPath, innerErr = assembleAndroidTestAPK(ctx, prj, mod, variant, manifestPath, outputs.testClassesDir, outputs.runtimeClasspath, stdout, stderr, c.tracker)
		return innerErr
	})
	if err != nil {
		return "", err
	}
	fmt.Fprintf(stdout, "assembled %s androidTest APK: %s\n", outputs.variantName, apkPath)
	return apkPath, nil
}

func androidTestApplicationID(mod *project.Module, variantName string) string {
	if mod == nil {
		return ""
	}
	target := mod.ResolveVariant(variantName)
	appID := normalizeAndroidPackageName(mod.Namespace, firstNonEmpty(target.ApplicationID, mod.ApplicationID, mod.Namespace))
	if appID == "" {
		return ""
	}
	return appID + ".test"
}

func androidTestManifestForPackaging(prj *project.Project, mod *project.Module, variantName string) (string, error) {
	if mod == nil {
		return "", fmt.Errorf("module is required")
	}
	variant := mod.ResolveVariant(variantName)
	packageName := androidTestApplicationID(mod, variantName)
	targetPackage := normalizeAndroidPackageName(mod.Namespace, firstNonEmpty(variant.ApplicationID, mod.ApplicationID, mod.Namespace))
	if packageName == "" || targetPackage == "" {
		return "", fmt.Errorf("module %s missing application id for androidTest packaging", mod.Path)
	}
	runner := strings.TrimSpace(mod.TestInstrumentationRunner)
	if runner == "" {
		runner = "androidx.test.runner.AndroidJUnitRunner"
	}
	outPath := filepath.Join(prj.RootDir, "build", "grit", moduleOutputRelPath(mod.Path), variantName+"AndroidTest", "AndroidManifest.xml")
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", err
	}
	body := androidTestManifestXML(packageName, targetPackage, runner, firstNonEmpty(variant.MinSDK, mod.MinSDK), firstNonEmpty(variant.TargetSDK, mod.TargetSDK))
	if err := writeFileIfChanged(outPath, body); err != nil {
		return "", err
	}
	return outPath, nil
}

func normalizeAndroidPackageName(namespace, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, ".") {
		base := strings.TrimSpace(namespace)
		if base == "" {
			return value
		}
		return base + value
	}
	return value
}

func androidTestManifestXML(packageName, targetPackage, runner, minSDK, targetSDK string) []byte {
	type usesSDK struct {
		XMLName          xml.Name `xml:"uses-sdk"`
		MinSDKVersion    string   `xml:"android:minSdkVersion,attr,omitempty"`
		TargetSDKVersion string   `xml:"android:targetSdkVersion,attr,omitempty"`
	}
	type instrumentation struct {
		XMLName       xml.Name `xml:"instrumentation"`
		Name          string   `xml:"android:name,attr"`
		TargetPackage string   `xml:"android:targetPackage,attr"`
	}
	type application struct {
		XMLName xml.Name `xml:"application"`
		Label   string   `xml:"android:label,attr,omitempty"`
	}
	type manifest struct {
		XMLName         xml.Name        `xml:"manifest"`
		XMLNSAndroid    string          `xml:"xmlns:android,attr"`
		Package         string          `xml:"package,attr"`
		UsesSDK         *usesSDK        `xml:"uses-sdk,omitempty"`
		Application     application     `xml:"application"`
		Instrumentation instrumentation `xml:"instrumentation"`
	}
	doc := manifest{
		XMLNSAndroid: "http://schemas.android.com/apk/res/android",
		Package:      packageName,
		Application:  application{Label: "androidTest"},
		Instrumentation: instrumentation{
			Name:          runner,
			TargetPackage: targetPackage,
		},
	}
	if minSDK != "" || targetSDK != "" {
		doc.UsesSDK = &usesSDK{MinSDKVersion: minSDK, TargetSDKVersion: targetSDK}
	}
	data, _ := xml.MarshalIndent(doc, "", "  ")
	return append([]byte(xml.Header), append(data, '\n')...)
}
