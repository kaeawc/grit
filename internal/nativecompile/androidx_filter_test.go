package nativecompile

import (
	"testing"

	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/project"
)

func TestFilterResolvedForProjectDropsLegacySupportArtifactsWhenUsingAndroidX(t *testing.T) {
	t.Parallel()

	prj := &project.Project{GradleProperties: map[string]string{"android.useAndroidX": "true"}}
	resolved := &m2local.Resolved{
		CompileJars: []string{
			"/Users/jason/.grit/aar/com.android.support/support-compat/26.1.0/classes.jar",
			"/Users/jason/.grit/aar/androidx.core/core/1.17.0/classes.jar",
		},
		RuntimeJars: []string{
			"/Users/jason/.gradle/caches/modules-2/files-2.1/com.android.support/support-annotations/26.1.0/downloaded/support-annotations-26.1.0.jar",
			"/Users/jason/.gradle/caches/modules-2/files-2.1/androidx.annotation/annotation-jvm/1.9.1/x/annotation-jvm-1.9.1.jar",
		},
		AndroidLibraries: []m2local.AndroidLibrary{
			{ID: "maven:com.android.support:support-compat:26.1.0"},
			{ID: "maven:androidx.core:core:1.17.0"},
		},
	}

	got := filterResolvedForProject(prj, resolved)
	if len(got.CompileJars) != 1 || got.CompileJars[0] != resolved.CompileJars[1] {
		t.Fatalf("unexpected compile jars: %#v", got.CompileJars)
	}
	if len(got.RuntimeJars) != 1 || got.RuntimeJars[0] != resolved.RuntimeJars[1] {
		t.Fatalf("unexpected runtime jars: %#v", got.RuntimeJars)
	}
	if len(got.AndroidLibraries) != 1 || got.AndroidLibraries[0].ID != "maven:androidx.core:core:1.17.0" {
		t.Fatalf("unexpected android libraries: %#v", got.AndroidLibraries)
	}
}
