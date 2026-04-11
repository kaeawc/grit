package testsupport

import "testing"

func TestCommandRunnerRecordsAndReplaysResponses(t *testing.T) {
	t.Parallel()

	runner := NewCommandRunner()
	runner.When("adb", []string{"install", "-r", "app.apk"}, CommandResponse{Stdout: "ok"})

	got := runner.Run("adb", "install", "-r", "app.apk")
	if got.Stdout != "ok" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if got, want := runner.Count("adb"), 1; got != want {
		t.Fatalf("unexpected adb call count: got %d want %d", got, want)
	}
	if len(runner.CallsSnapshot()) != 1 || runner.CallsSnapshot()[0].Name != "adb" {
		t.Fatalf("unexpected call log: %#v", runner.CallsSnapshot())
	}
}

func TestADBInstallArgsRoundTrip(t *testing.T) {
	t.Parallel()

	args := ADBInstallArgs("device-123", "app.apk")
	if !IsADBInstallInvocation("adb", args) {
		t.Fatalf("expected adb install invocation, got %#v", args)
	}
	invocation, ok := ParseADBInstallArgs(args)
	if !ok {
		t.Fatalf("expected args to parse, got %#v", args)
	}
	if invocation.DeviceSerial != "device-123" || invocation.APKPath != "app.apk" {
		t.Fatalf("unexpected parsed invocation: %#v", invocation)
	}
}
