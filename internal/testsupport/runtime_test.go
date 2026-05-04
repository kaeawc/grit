package testsupport

import "testing"

func TestCommandRunnerRecorderCapturesCalls(t *testing.T) {
	runner := &CommandRunnerRecorder{}
	if err := runner.Run("zip", "-q", "app.apk"); err != nil {
		t.Fatal(err)
	}
	if len(runner.Calls) != 1 {
		t.Fatalf("calls = %#v", runner.Calls)
	}
	if runner.Calls[0].Name != "zip" || len(runner.Calls[0].Args) != 2 {
		t.Fatalf("unexpected call = %#v", runner.Calls[0])
	}
}

func TestDeviceRecorderCapturesInstallAndUninstall(t *testing.T) {
	device := &DeviceRecorder{}
	if err := device.Install("app.apk"); err != nil {
		t.Fatal(err)
	}
	if err := device.Uninstall("dev.example.app"); err != nil {
		t.Fatal(err)
	}
	if len(device.Installs) != 1 || device.Installs[0] != "app.apk" {
		t.Fatalf("installs = %#v", device.Installs)
	}
	if len(device.Uninstalls) != 1 || device.Uninstalls[0] != "dev.example.app" {
		t.Fatalf("uninstalls = %#v", device.Uninstalls)
	}
}

func TestArtifactStoreRecorderRoundTrips(t *testing.T) {
	store := NewArtifactStoreRecorder()
	store.Save("compile:app:debug", []byte("cached"))
	got, ok := store.Load("compile:app:debug")
	if !ok || string(got) != "cached" {
		t.Fatalf("load = %q %v", got, ok)
	}
	if len(store.Saves) != 1 || len(store.Loads) != 1 {
		t.Fatalf("saves = %#v loads = %#v", store.Saves, store.Loads)
	}
}

func TestDependencyResolverRecorderReturnsConfiguredResults(t *testing.T) {
	resolver := NewDependencyResolverRecorder()
	resolver.Results[":app"] = []string{":lib", "org.example:dep"}
	got := resolver.Resolve(":app")
	if len(got) != 2 || got[0] != ":lib" || got[1] != "org.example:dep" {
		t.Fatalf("got = %#v", got)
	}
	if len(resolver.Queries) != 1 || resolver.Queries[0] != ":app" {
		t.Fatalf("queries = %#v", resolver.Queries)
	}
}
