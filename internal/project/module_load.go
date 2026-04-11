package project

import (
	"os"
	"path/filepath"
	"strings"
)

func loadModule(prj *Project, modulePath string) (*Module, error) {
	rel := strings.TrimPrefix(strings.ReplaceAll(modulePath, ":", "/"), "/")
	if prj.ModuleDirs != nil {
		if configured := strings.TrimSpace(prj.ModuleDirs[modulePath]); configured != "" {
			rel = configured
		}
	}
	modDir := filepath.Join(prj.RootDir, rel)
	buildFile := firstExisting(
		filepath.Join(modDir, "build.gradle.kts"),
		filepath.Join(modDir, "build.gradle"),
	)
	if !fileExists(buildFile) {
		return &Module{Path: modulePath, Dir: modDir, BuildFile: filepath.Join(modDir, "build.gradle.kts")}, nil
	}

	data, err := os.ReadFile(buildFile)
	if err != nil {
		return nil, err
	}
	body := string(data)
	defaultConfig := parseDefaultConfig(prj, body)

	mod := &Module{
		Path:          modulePath,
		Dir:           modDir,
		BuildFile:     buildFile,
		Type:          detectModuleType(body),
		Namespace:     parseAssignment(body, `namespace\s*=\s*"([^"]+)"`),
		ApplicationID: firstNonEmpty(defaultConfig.ApplicationID, parseAssignment(body, `applicationId\s*=\s*"([^"]+)"`)),
		VersionCode:   firstNonEmpty(defaultConfig.VersionCode, parseAssignment(body, `versionCode\s*=\s*([0-9]+)`)),
		VersionName:   firstNonEmpty(defaultConfig.VersionName, parseAssignment(body, `versionName\s*=\s*"([^"]+)"`)),
		CompileSDK: resolveCatalogValue(prj, firstNonEmpty(
			parseAssignment(body, `compileSdk\s*=\s*(\d+)`),
			parseCatalogRef(body, `compileSdk = libs\.versions\.([A-Za-z0-9\.\-_]+)\.get\(\)\.toInt\(\)`),
		)),
		BuildToolsVersion: resolveCatalogValue(prj, firstNonEmpty(
			parseAssignment(body, `buildToolsVersion\s*=\s*"([^"]+)"`),
			parseCatalogRef(body, `buildToolsVersion = libs\.versions\.([A-Za-z0-9\.\-_]+)\.get\(\)`),
		)),
		MinSDK: firstNonEmpty(defaultConfig.MinSDK, resolveCatalogValue(prj, firstNonEmpty(
			parseAssignment(body, `minSdk\s*=\s*(\d+)`),
			parseCatalogRef(body, `minSdk = libs\.versions\.([A-Za-z0-9\.\-_]+)\.get\(\)\.toInt\(\)`),
		))),
		TargetSDK: firstNonEmpty(defaultConfig.TargetSDK, resolveCatalogValue(prj, firstNonEmpty(
			parseAssignment(body, `targetSdk\s*=\s*(\d+)`),
			parseCatalogRef(body, `targetSdk = libs\.versions\.([A-Za-z0-9\.\-_]+)\.get\(\)\.toInt\(\)`),
		))),
		UsesCompose:               strings.Contains(body, "compose = true") || strings.Contains(body, "compose-compiler"),
		UsesKotlinSerialization:   strings.Contains(body, "libs.plugins.kotlin.serialization"),
		UsesMetro:                 strings.Contains(body, "libs.plugins.metro"),
		TestInstrumentationRunner: parseAssignment(body, `testInstrumentationRunner\s*=\s*"([^"]+)"`),
		KotlinFreeCompilerArgs:    parseFreeCompilerArgs(body),
		LintDisabledChecks:        parseLintDisabledChecks(body),
		ConsumerProguardFiles:     resolveRelativeFiles(modDir, parseQuotedListArgs(body, "consumerProguardFiles")),
		SigningConfigs:            parseSigningConfigs(prj, body, modDir),
		DefaultConfig:             defaultConfig,
		FlavorDimensions:          parseFlavorDimensions(body),
		ProductFlavors:            parseProductFlavors(body),
		BuildTypes:                mergeBuildTypeMaps(parseBuildTypes(body, modDir), parseCustomVariants(body, modDir)),
	}

	mod.SourceFileCount = countFiles(filepath.Join(modDir, "src", "main"), func(path string) bool {
		return strings.HasSuffix(path, ".kt") || strings.HasSuffix(path, ".java")
	})
	mod.UnitTestFileCount = countFiles(filepath.Join(modDir, "src", "test"), func(path string) bool {
		return strings.HasSuffix(path, ".kt") || strings.HasSuffix(path, ".java")
	})
	mod.AndroidTestFileCount = countFiles(filepath.Join(modDir, "src", "androidTest"), func(path string) bool {
		return strings.HasSuffix(path, ".kt") || strings.HasSuffix(path, ".java")
	})

	return mod, nil
}

func detectModuleType(body string) string {
	switch {
	case strings.Contains(body, "libs.plugins.android.application"),
		strings.Contains(body, `id("com.android.application")`),
		strings.Contains(body, `id 'com.android.application'`),
		strings.Contains(body, `id("signal-sample-app")`),
		strings.Contains(body, `id 'signal-sample-app'`),
		strings.Contains(body, `id("com.android.test")`):
		return "android-application"
	case strings.Contains(body, "libs.plugins.android.library"),
		strings.Contains(body, `id("signal-library")`),
		strings.Contains(body, `id 'signal-library'`),
		strings.Contains(body, `id("com.android.library")`):
		return "android-library"
	case strings.Contains(body, `kotlin("jvm")`),
		strings.Contains(body, `id("org.jetbrains.kotlin.jvm")`),
		strings.Contains(body, `id 'org.jetbrains.kotlin.jvm'`),
		strings.Contains(body, "`java-library`"),
		strings.Contains(body, `id("java-library")`):
		return "jvm-library"
	default:
		return ""
	}
}
