package project

import "testing"

func TestQualityTasksExposeKtlintAndDetektAsUnsupported(t *testing.T) {
	mod := Module{
		Path:             ":app",
		Type:             "android-application",
		Plugins:          []string{"org.jlleitschuh.gradle.ktlint", "dev.detekt"},
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]ProductFlavor{
			"free": {Name: "free", Dimension: "tier"},
		},
		BuildTypes: map[string]BuildType{
			"debug": {Name: "debug"},
		},
	}

	tasks := mod.Tasks()
	for _, name := range []string{
		"ktlintCheck",
		"ktlintFormat",
		"ktlintGenerateBaseline",
		"runKtlintCheckOverFreeDebugSourceSet",
		"runKtlintFormatOverTestFreeDebugSourceSet",
		"detekt",
		"detektBaseline",
		"detektFreeDebug",
		"detektFreeDebugUnitTest",
		"detektBaselineFreeDebugSourceSet",
	} {
		if !hasTask(tasks, name) {
			t.Fatalf("expected quality task %q in %#v", name, tasks)
		}
		if taskSupported(tasks, name) {
			t.Fatalf("expected quality task %q to be unsupported", name)
		}
	}
}

func TestQualityTasksUseJvmTestTaskNames(t *testing.T) {
	mod := Module{
		Path:    ":lib",
		Type:    "jvm-library",
		Plugins: []string{"org.jlleitschuh.gradle.ktlint", "dev.detekt"},
	}

	tasks := mod.Tasks()
	for _, name := range []string{
		"runKtlintCheckOverTestSourceSet",
		"runKtlintFormatOverTestSourceSet",
		"detektTest",
		"detektTestSourceSet",
		"detektBaselineTest",
		"detektBaselineTestSourceSet",
		"detektGenerateConfig",
	} {
		if !hasTask(tasks, name) {
			t.Fatalf("expected JVM quality task %q in %#v", name, tasks)
		}
	}
	if hasTask(tasks, "detektMainUnitTest") || hasTask(tasks, "runKtlintCheckOverTestMainSourceSet") {
		t.Fatalf("unexpected Android-style JVM quality task names in %#v", tasks)
	}
}
