package nativecompile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
)

func assembleAPK(ctx context.Context, prj *project.Project, mod *project.Module, variant project.BuildType, classesDir string, runtimeCP []string, resources []androidResourceArtifact, stdout, stderr *os.File, tracker perf.Tracker) (string, error) {
	dexState := newCompileState()
	variantDir := moduleOutputRelPath(mod.Path)
	outRoot := filepath.Join(prj.RootDir, "build", "grit", variantDir, variant.Name)
	dexDir := filepath.Join(outRoot, "dex")
	libDexDir := filepath.Join(outRoot, "lib-dex")
	appDexDir := filepath.Join(outRoot, "app-dex")
	classesJar := filepath.Join(outRoot, "app-classes.jar")
	unsignedAPK := filepath.Join(outRoot, "app-"+variant.Name+"-unsigned.apk")
	unalignedAPK := filepath.Join(outRoot, "app-"+variant.Name+"-unaligned.apk")
	finalAPK := filepath.Join(outRoot, "app-"+variant.Name+".apk")
	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		return "", err
	}

	if err := tracker.Track("aapt2Link", func() error {
		outAPKInputs := append([]string{manifestForPackagingPathOrEmpty(prj, mod, variant.Name), androidJarPath()}, resourceArtifactStamps(resources)...)
		if outputsNewerThanInputs(unalignedAPK, outAPKInputs) {
			recordCacheProbe(tracker, "aapt2Link", true, "local-up-to-date", "linked resource apk newer than manifest and resource inputs")
		} else {
			recordCacheProbe(tracker, "aapt2Link", false, "cache-miss", "linked resource apk required fresh aapt2 link")
		}
		return runAAPT2Link(ctx, prj, mod, variant, flattenCompiledResourceFiles(resources), resourceArtifactStamps(resources), unalignedAPK, stdout, stderr)
	}); err != nil {
		return "", err
	}
	if err := tracker.Track("jarClasses", func() error {
		jarStampPath := classesJar + ".stamp"
		jarStampValue := classesJarStampValue(classesDir)
		if stampMatches(jarStampPath, jarStampValue) && pathIsFile(classesJar) {
			recordCacheProbe(tracker, "jarClasses", true, "local-up-to-date", "class jar stamp matched local outputs")
			return nil
		}
		if outputsNewerThanInputs(classesJar, []string{classesDir}) {
			_ = writeStamp(jarStampPath, jarStampValue)
			recordCacheProbe(tracker, "jarClasses", true, "local-up-to-date", "class jar newer than classes directory")
			return nil
		}
		recordCacheProbe(tracker, "jarClasses", false, "cache-miss", "class jar required fresh packaging")
		if err := jarClasses(ctx, classesDir, classesJar, stdout, stderr); err != nil {
			return err
		}
		return writeStamp(jarStampPath, jarStampValue)
	}); err != nil {
		return "", err
	}
	if variant.IsMinifyEnabled {
		tc, err := dexState.dexToolchainForProject(prj)
		if err != nil {
			return "", err
		}
		if err := tracker.Track("runR8", func() error {
			return runR8(ctx, tc, mod, variant, classesJar, dexDir, runtimeCP, stdout, stderr)
		}); err != nil {
			return "", err
		}
	} else if err := tracker.Track("runD8", func() error {
		tc, err := dexState.dexToolchainForProject(prj)
		if err != nil {
			return err
		}
		inputs := append([]string{classesJar}, runtimeCP...)
		if dexOutputsFresh(dexDir, inputs, tc) {
			recordCacheProbe(tracker, "runD8", true, "local-up-to-date", "dex outputs newer than classes and runtime classpath")
		} else {
			recordCacheProbe(tracker, "runD8", false, "cache-miss", "dex outputs required D8 execution")
		}
		return runD8(ctx, tc, prj.RootDir, classesJar, appDexDir, libDexDir, dexDir, runtimeCP, stdout, stderr)
	}); err != nil {
		return "", err
	}
	if err := tracker.Track("copyUnsignedAPK", func() error {
		if outputsNewerThanInputs(unsignedAPK, []string{unalignedAPK}) {
			recordCacheProbe(tracker, "copyUnsignedAPK", true, "local-up-to-date", "unsigned apk newer than unaligned apk")
			return nil
		}
		recordCacheProbe(tracker, "copyUnsignedAPK", false, "cache-miss", "unsigned apk required refresh from unaligned apk")
		return copyFile(unalignedAPK, unsignedAPK)
	}); err != nil {
		return "", err
	}
	if err := tracker.Track("addDexToAPK", func() error {
		if outputsNewerThanInputs(unsignedAPK, []string{dexDir}) {
			recordCacheProbe(tracker, "addDexToAPK", true, "local-up-to-date", "apk already newer than dex directory")
		} else {
			recordCacheProbe(tracker, "addDexToAPK", false, "cache-miss", "apk required dex insertion")
		}
		return addDexToAPK(ctx, unsignedAPK, dexDir, stdout, stderr)
	}); err != nil {
		return "", err
	}
	if err := tracker.Track("signAPK", func() error {
		signingName, signing := selectSigningConfig(mod, variant)
		if signingName == "" {
			if outputsNewerThanInputs(finalAPK, []string{unsignedAPK}) {
				recordCacheProbe(tracker, "signAPK", true, "local-up-to-date", "unsigned apk copied previously and final apk is current")
			} else {
				recordCacheProbe(tracker, "signAPK", false, "cache-miss", "final apk required unsigned copy")
			}
		} else if restoreSharedSignedAPKPreview(unsignedAPK, signingName, signing, finalAPK) {
			recordCacheProbe(tracker, "signAPK", true, "shared-cache-hit", "restored signed apk from shared cache")
		} else if outputsNewerThanInputs(finalAPK, []string{unsignedAPK, signing.StoreFile}) {
			recordCacheProbe(tracker, "signAPK", true, "local-up-to-date", "signed apk newer than unsigned apk and keystore")
		} else {
			recordCacheProbe(tracker, "signAPK", false, "cache-miss", "signing required apksigner execution")
		}
		return signAPK(ctx, mod, variant, unsignedAPK, finalAPK, stdout, stderr)
	}); err != nil {
		return "", err
	}
	return finalAPK, nil
}

func assembleAndroidTestAPK(ctx context.Context, prj *project.Project, mod *project.Module, variant project.BuildType, manifestPath, classesDir string, runtimeCP []string, stdout, stderr *os.File, tracker perf.Tracker) (string, error) {
	dexState := newCompileState()
	variantDir := moduleOutputRelPath(mod.Path)
	outRoot := filepath.Join(prj.RootDir, "build", "grit", variantDir, variant.Name+"AndroidTest")
	dexDir := filepath.Join(outRoot, "dex")
	libDexDir := filepath.Join(outRoot, "lib-dex")
	appDexDir := filepath.Join(outRoot, "app-dex")
	classesJar := filepath.Join(outRoot, "android-test-classes.jar")
	unsignedAPK := filepath.Join(outRoot, "app-"+variant.Name+"-androidTest-unsigned.apk")
	unalignedAPK := filepath.Join(outRoot, "app-"+variant.Name+"-androidTest-unaligned.apk")
	finalAPK := filepath.Join(outRoot, "app-"+variant.Name+"-androidTest.apk")
	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		return "", err
	}
	if err := tracker.Track("aapt2LinkAndroidTest", func() error {
		if outputsNewerThanInputs(unalignedAPK, []string{manifestPath, androidJarPath()}) {
			recordCacheProbe(tracker, "aapt2LinkAndroidTest", true, "local-up-to-date", "linked androidTest apk newer than manifest input")
		} else {
			recordCacheProbe(tracker, "aapt2LinkAndroidTest", false, "cache-miss", "linked androidTest apk required fresh aapt2 link")
		}
		debugMode := strings.EqualFold(strings.TrimSpace(firstNonEmpty(variant.BaseBuildType, variant.Name)), "debug") || strings.HasSuffix(strings.TrimSpace(variant.Name), "Debug")
		return runAAPT2LinkWithManifest(ctx, manifestPath, mod.MinSDK, mod.TargetSDK, debugMode, nil, nil, unalignedAPK, stdout, stderr)
	}); err != nil {
		return "", err
	}
	if err := tracker.Track("jarAndroidTestClasses", func() error {
		jarStampPath := classesJar + ".stamp"
		jarStampValue := classesJarStampValue(classesDir)
		if stampMatches(jarStampPath, jarStampValue) && pathIsFile(classesJar) {
			recordCacheProbe(tracker, "jarAndroidTestClasses", true, "local-up-to-date", "androidTest class jar stamp matched local outputs")
			return nil
		}
		if outputsNewerThanInputs(classesJar, []string{classesDir}) {
			_ = writeStamp(jarStampPath, jarStampValue)
			recordCacheProbe(tracker, "jarAndroidTestClasses", true, "local-up-to-date", "androidTest class jar newer than classes directory")
			return nil
		}
		recordCacheProbe(tracker, "jarAndroidTestClasses", false, "cache-miss", "androidTest class jar required fresh packaging")
		if err := jarClasses(ctx, classesDir, classesJar, stdout, stderr); err != nil {
			return err
		}
		return writeStamp(jarStampPath, jarStampValue)
	}); err != nil {
		return "", err
	}
	if err := tracker.Track("runD8AndroidTest", func() error {
		tc, err := dexState.dexToolchainForProject(prj)
		if err != nil {
			return err
		}
		inputs := append([]string{classesJar}, runtimeCP...)
		if dexOutputsFresh(dexDir, inputs, tc) {
			recordCacheProbe(tracker, "runD8AndroidTest", true, "local-up-to-date", "androidTest dex outputs newer than classes and runtime classpath")
		} else {
			recordCacheProbe(tracker, "runD8AndroidTest", false, "cache-miss", "androidTest dex outputs required D8 execution")
		}
		return runD8(ctx, tc, prj.RootDir, classesJar, appDexDir, libDexDir, dexDir, runtimeCP, stdout, stderr)
	}); err != nil {
		return "", err
	}
	if err := tracker.Track("copyUnsignedAndroidTestAPK", func() error {
		if outputsNewerThanInputs(unsignedAPK, []string{unalignedAPK}) {
			recordCacheProbe(tracker, "copyUnsignedAndroidTestAPK", true, "local-up-to-date", "unsigned androidTest apk newer than unaligned apk")
			return nil
		}
		recordCacheProbe(tracker, "copyUnsignedAndroidTestAPK", false, "cache-miss", "unsigned androidTest apk required refresh from unaligned apk")
		return copyFile(unalignedAPK, unsignedAPK)
	}); err != nil {
		return "", err
	}
	if err := tracker.Track("addDexToAndroidTestAPK", func() error {
		if outputsNewerThanInputs(unsignedAPK, []string{dexDir}) {
			recordCacheProbe(tracker, "addDexToAndroidTestAPK", true, "local-up-to-date", "androidTest apk already newer than dex directory")
		} else {
			recordCacheProbe(tracker, "addDexToAndroidTestAPK", false, "cache-miss", "androidTest apk required dex insertion")
		}
		return addDexToAPK(ctx, unsignedAPK, dexDir, stdout, stderr)
	}); err != nil {
		return "", err
	}
	if err := tracker.Track("signAndroidTestAPK", func() error {
		signingName, signing := selectSigningConfig(mod, variant)
		if signingName == "" {
			if outputsNewerThanInputs(finalAPK, []string{unsignedAPK}) {
				recordCacheProbe(tracker, "signAndroidTestAPK", true, "local-up-to-date", "unsigned androidTest apk copied previously and final apk is current")
			} else {
				recordCacheProbe(tracker, "signAndroidTestAPK", false, "cache-miss", "final androidTest apk required unsigned copy")
			}
		} else if restoreSharedSignedAPKPreview(unsignedAPK, signingName, signing, finalAPK) {
			recordCacheProbe(tracker, "signAndroidTestAPK", true, "shared-cache-hit", "restored signed androidTest apk from shared cache")
		} else if outputsNewerThanInputs(finalAPK, []string{unsignedAPK, signing.StoreFile}) {
			recordCacheProbe(tracker, "signAndroidTestAPK", true, "local-up-to-date", "signed androidTest apk newer than unsigned apk and keystore")
		} else {
			recordCacheProbe(tracker, "signAndroidTestAPK", false, "cache-miss", "signing required apksigner execution for androidTest apk")
		}
		return signAPK(ctx, mod, variant, unsignedAPK, finalAPK, stdout, stderr)
	}); err != nil {
		return "", err
	}
	return finalAPK, nil
}

func assembleAAB(ctx context.Context, s *compileState, prj *project.Project, mod *project.Module, variant project.BuildType, classesDir string, runtimeCP []string, resources []androidResourceArtifact, stdout, stderr *os.File, tracker perf.Tracker) (string, error) {
	variantDir := moduleOutputRelPath(mod.Path)
	outRoot := filepath.Join(prj.RootDir, "build", "grit", variantDir, variant.Name)
	dexDir := filepath.Join(outRoot, "dex")
	libDexDir := filepath.Join(outRoot, "lib-dex")
	appDexDir := filepath.Join(outRoot, "app-dex")
	classesJar := filepath.Join(outRoot, "app-classes.jar")
	protoAPK := filepath.Join(outRoot, "proto-resources.apk")
	protoDir := filepath.Join(outRoot, "proto-extracted")
	moduleZipDir := filepath.Join(outRoot, "module-zip")
	baseZip := filepath.Join(moduleZipDir, "base.zip")
	unsignedAAB := filepath.Join(outRoot, "app-"+variant.Name+"-unsigned.aab")
	finalAAB := filepath.Join(outRoot, "app-"+variant.Name+".aab")

	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		return "", err
	}

	// Resolve bundletool toolchain.
	tc, err := s.bundletoolToolchainForProject(prj)
	if err != nil {
		return "", err
	}

	// Jar classes.
	if err := tracker.Track("jarClasses", func() error {
		jarStampPath := classesJar + ".stamp"
		jarStampValue := classesJarStampValue(classesDir)
		if stampMatches(jarStampPath, jarStampValue) && pathIsFile(classesJar) {
			recordCacheProbe(tracker, "jarClasses", true, "local-up-to-date", "class jar stamp matched local outputs")
			return nil
		}
		if outputsNewerThanInputs(classesJar, []string{classesDir}) {
			_ = writeStamp(jarStampPath, jarStampValue)
			recordCacheProbe(tracker, "jarClasses", true, "local-up-to-date", "class jar newer than classes directory")
			return nil
		}
		recordCacheProbe(tracker, "jarClasses", false, "cache-miss", "class jar required fresh packaging")
		if err := jarClasses(ctx, classesDir, classesJar, stdout, stderr); err != nil {
			return err
		}
		return writeStamp(jarStampPath, jarStampValue)
	}); err != nil {
		return "", err
	}

	// Dex.
	if variant.IsMinifyEnabled {
		tc, err := s.dexToolchainForProject(prj)
		if err != nil {
			return "", err
		}
		if err := tracker.Track("runR8", func() error {
			return runR8(ctx, tc, mod, variant, classesJar, dexDir, runtimeCP, stdout, stderr)
		}); err != nil {
			return "", err
		}
	} else if err := tracker.Track("runD8", func() error {
		tc, err := s.dexToolchainForProject(prj)
		if err != nil {
			return err
		}
		inputs := append([]string{classesJar}, runtimeCP...)
		if dexOutputsFresh(dexDir, inputs, tc) {
			recordCacheProbe(tracker, "runD8", true, "local-up-to-date", "dex outputs newer than classes and runtime classpath")
		} else {
			recordCacheProbe(tracker, "runD8", false, "cache-miss", "dex outputs required D8 execution")
		}
		return runD8(ctx, tc, prj.RootDir, classesJar, appDexDir, libDexDir, dexDir, runtimeCP, stdout, stderr)
	}); err != nil {
		return "", err
	}

	// Link resources in proto format for AAB module zip.
	compiledFiles := flattenCompiledResourceFiles(resources)
	if err := tracker.Track("aapt2LinkProto", func() error {
		if len(compiledFiles) == 0 {
			return nil
		}
		manifestPath := manifestForPackagingPathOrEmpty(prj, mod, variant.Name)
		if manifestPath == "" {
			return fmt.Errorf("assembleAAB: manifest not found for proto link %s/%s", mod.Path, variant.Name)
		}
		debugMode := strings.EqualFold(strings.TrimSpace(firstNonEmpty(variant.BaseBuildType, variant.Name)), "debug") || strings.HasSuffix(strings.TrimSpace(variant.Name), "Debug")
		outInputs := append([]string{manifestPath}, resourceArtifactStamps(resources)...)
		if outputsNewerThanInputs(protoAPK, outInputs) {
			recordCacheProbe(tracker, "aapt2LinkProto", true, "local-up-to-date", "proto resource APK newer than manifest and resource inputs")
			return nil
		}
		recordCacheProbe(tracker, "aapt2LinkProto", false, "cache-miss", "proto resource APK required fresh aapt2 link")
		return runAAPT2LinkProto(ctx, manifestPath, mod.MinSDK, mod.TargetSDK, debugMode, compiledFiles, resourceArtifactStamps(resources), protoAPK, stdout, stderr)
	}); err != nil {
		return "", err
	}

	// Extract proto APK if resources were linked.
	if err := tracker.Track("extractProtoAPK", func() error {
		if len(compiledFiles) == 0 || !pathIsFile(protoAPK) {
			return nil
		}
		if outputsNewerThanInputs(protoDir, []string{protoAPK}) {
			recordCacheProbe(tracker, "extractProtoAPK", true, "local-up-to-date", "extracted proto dir newer than proto APK")
			return nil
		}
		recordCacheProbe(tracker, "extractProtoAPK", false, "cache-miss", "proto APK extraction required")
		if err := os.RemoveAll(protoDir); err != nil {
			return err
		}
		if err := os.MkdirAll(protoDir, 0o755); err != nil {
			return err
		}
		return extractProtoAPK(protoAPK, protoDir)
	}); err != nil {
		return "", err
	}

	// Assemble module zip.
	if err := tracker.Track("assembleModuleZip", func() error {
		// Use proto-format manifest if available (from aapt2 link --proto-format),
		// otherwise fall back to the source manifest.
		manifestPath := manifestForPackagingPathOrEmpty(prj, mod, variant.Name)
		if manifestPath == "" {
			return fmt.Errorf("assembleAAB: manifest not found for %s/%s", mod.Path, variant.Name)
		}
		protoManifest := filepath.Join(protoDir, "AndroidManifest.xml")
		if pathIsFile(protoManifest) {
			manifestPath = protoManifest
		}
		inputs := moduleZipInputs{
			ManifestPath: manifestPath,
			DexDir:       dexDir,
		}
		// Include proto resource table and compiled resources if available.
		resourcesTable := filepath.Join(protoDir, "resources.pb")
		if pathIsFile(resourcesTable) {
			inputs.ResourceTablePath = resourcesTable
		}
		protoResDir := filepath.Join(protoDir, "res")
		if pathIsDir(protoResDir) {
			inputs.ResourceDir = protoResDir
		}
		// Discover assets directory if it exists.
		assetsDir := filepath.Join(mod.Dir, "src", "main", "assets")
		if pathIsDir(assetsDir) {
			inputs.AssetsDir = assetsDir
		}
		// Discover JNI libs directory if it exists.
		jniLibsDir := filepath.Join(mod.Dir, "src", "main", "jniLibs")
		if pathIsDir(jniLibsDir) {
			entries, dirErr := os.ReadDir(jniLibsDir)
			if dirErr == nil {
				nativeLibs := make(map[string]string)
				for _, e := range entries {
					if e.IsDir() {
						nativeLibs[e.Name()] = filepath.Join(jniLibsDir, e.Name())
					}
				}
				if len(nativeLibs) > 0 {
					inputs.NativeLibDirs = nativeLibs
				}
			}
		}
		zipInputs := []string{inputs.ManifestPath, dexDir}
		if inputs.ResourceTablePath != "" {
			zipInputs = append(zipInputs, inputs.ResourceTablePath)
		}
		if inputs.ResourceDir != "" {
			zipInputs = append(zipInputs, inputs.ResourceDir)
		}
		if outputsNewerThanInputs(baseZip, zipInputs) {
			recordCacheProbe(tracker, "assembleModuleZip", true, "local-up-to-date", "module zip newer than manifest and dex inputs")
			return nil
		}
		recordCacheProbe(tracker, "assembleModuleZip", false, "cache-miss", "module zip required fresh assembly")
		return assembleModuleZip(inputs, baseZip)
	}); err != nil {
		return "", err
	}

	// Discover optional BundleConfig.
	bundleConfigPath := findBundleConfig(mod.Dir)

	// Compute shared cache directory for the AAB assembly.
	aabCacheDir := aabAssemblyCacheDir(tc, []string{baseZip}, bundleConfigPath)

	// Run bundletool build-bundle.
	if err := tracker.Track("bundletoolBuildBundle", func() error {
		inputs := []string{baseZip}
		if bundleConfigPath != "" {
			inputs = append(inputs, bundleConfigPath)
		}
		if outputsNewerThanInputs(unsignedAAB, inputs) {
			recordCacheProbe(tracker, "bundletoolBuildBundle", true, "local-up-to-date", "unsigned AAB newer than module zip")
			return nil
		}
		if restoreSharedAABAssembly(unsignedAAB, aabCacheDir) {
			recordCacheProbe(tracker, "bundletoolBuildBundle", true, "shared-cache-hit", "restored unsigned AAB from shared cache")
			return nil
		}
		recordCacheProbe(tracker, "bundletoolBuildBundle", false, "cache-miss", "unsigned AAB required bundletool execution")
		if err := runBundletoolBuildBundle(ctx, tc, []string{baseZip}, unsignedAAB, bundleConfigPath, stdout, stderr); err != nil {
			return err
		}
		_ = saveSharedAABAssembly(unsignedAAB, aabCacheDir)
		return nil
	}); err != nil {
		return "", err
	}

	// Sign AAB.
	if err := tracker.Track("signAAB", func() error {
		signingName, signing := selectSigningConfig(mod, variant)
		if signingName == "" {
			if outputsNewerThanInputs(finalAAB, []string{unsignedAAB}) {
				recordCacheProbe(tracker, "signAAB", true, "local-up-to-date", "unsigned AAB copied previously and final AAB is current")
			} else {
				recordCacheProbe(tracker, "signAAB", false, "cache-miss", "final AAB required unsigned copy")
			}
		} else if restoreSharedSignedAABPreview(unsignedAAB, signingName, signing) {
			recordCacheProbe(tracker, "signAAB", true, "shared-cache-hit", "restored signed AAB from shared cache")
		} else if outputsNewerThanInputs(finalAAB, []string{unsignedAAB, signing.StoreFile}) {
			recordCacheProbe(tracker, "signAAB", true, "local-up-to-date", "signed AAB newer than unsigned AAB and keystore")
		} else {
			recordCacheProbe(tracker, "signAAB", false, "cache-miss", "signing required jarsigner execution for AAB")
		}
		return signAAB(ctx, mod, variant, unsignedAAB, finalAAB, stdout, stderr)
	}); err != nil {
		return "", err
	}

	return finalAAB, nil
}

func manifestForPackagingPathOrEmpty(prj *project.Project, mod *project.Module, variantName string) string {
	path, err := manifestForPackagingForProject(prj, mod, variantName)
	if err != nil {
		return ""
	}
	return path
}
