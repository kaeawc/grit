package nativecompile

import "testing"

func TestUnitTestRunCachePathChangesWithClasspathIdentity(t *testing.T) {
	t.Parallel()

	tests := []string{"com.example.WidgetTest"}
	cp1 := []string{t.TempDir()}
	cp2 := []string{t.TempDir()}

	first := unitTestRunCachePath("/repo", ":app", "debug", tests, cp1)
	second := unitTestRunCachePath("/repo", ":app", "debug", tests, cp2)
	if first == second {
		t.Fatalf("expected cache path to differ for different classpath identities")
	}
}
