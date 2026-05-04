package nativecompile

import (
	"testing"
)

func TestFilterDevices_MinSDK(t *testing.T) {
	devices := []Device{
		{Serial: "low", APILevel: 21, ABIs: []string{"arm64-v8a"}},
		{Serial: "mid", APILevel: 28, ABIs: []string{"arm64-v8a"}},
		{Serial: "high", APILevel: 33, ABIs: []string{"arm64-v8a"}},
	}
	got := FilterDevices(devices, DeviceConstraints{MinSDK: 28})
	if len(got) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(got))
	}
	if got[0].Serial != "mid" || got[1].Serial != "high" {
		t.Fatalf("unexpected devices: %v", got)
	}
}

func TestFilterDevices_ABI(t *testing.T) {
	devices := []Device{
		{Serial: "arm", APILevel: 30, ABIs: []string{"arm64-v8a"}},
		{Serial: "x86", APILevel: 30, ABIs: []string{"x86_64"}},
		{Serial: "both", APILevel: 30, ABIs: []string{"arm64-v8a", "x86_64"}},
	}
	got := FilterDevices(devices, DeviceConstraints{RequiredABIs: []string{"x86_64"}})
	if len(got) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(got))
	}
	if got[0].Serial != "x86" || got[1].Serial != "both" {
		t.Fatalf("unexpected devices: %v", got)
	}
}

func TestFilterDevices_MinSDKAndABI(t *testing.T) {
	devices := []Device{
		{Serial: "old-arm", APILevel: 21, ABIs: []string{"arm64-v8a"}},
		{Serial: "new-arm", APILevel: 30, ABIs: []string{"arm64-v8a"}},
		{Serial: "new-x86", APILevel: 30, ABIs: []string{"x86_64"}},
	}
	got := FilterDevices(devices, DeviceConstraints{MinSDK: 28, RequiredABIs: []string{"arm64-v8a"}})
	if len(got) != 1 {
		t.Fatalf("expected 1 device, got %d", len(got))
	}
	if got[0].Serial != "new-arm" {
		t.Fatalf("expected new-arm, got %s", got[0].Serial)
	}
}

func TestFilterDevices_NoConstraints(t *testing.T) {
	devices := []Device{
		{Serial: "a", APILevel: 21, ABIs: []string{"arm64-v8a"}},
		{Serial: "b", APILevel: 30, ABIs: []string{"x86_64"}},
	}
	got := FilterDevices(devices, DeviceConstraints{})
	if len(got) != 2 {
		t.Fatalf("expected all 2 devices, got %d", len(got))
	}
}

func TestFilterDevices_NoneMatch(t *testing.T) {
	devices := []Device{
		{Serial: "old", APILevel: 21, ABIs: []string{"armeabi-v7a"}},
	}
	got := FilterDevices(devices, DeviceConstraints{MinSDK: 30, RequiredABIs: []string{"arm64-v8a"}})
	if len(got) != 0 {
		t.Fatalf("expected 0 devices, got %d", len(got))
	}
}

func TestFilterDevices_EmptyInput(t *testing.T) {
	got := FilterDevices(nil, DeviceConstraints{MinSDK: 21})
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestFilterDevices_MultipleRequiredABIs(t *testing.T) {
	devices := []Device{
		{Serial: "arm-only", APILevel: 30, ABIs: []string{"arm64-v8a"}},
		{Serial: "x86-only", APILevel: 30, ABIs: []string{"x86_64"}},
		{Serial: "neither", APILevel: 30, ABIs: []string{"armeabi-v7a"}},
	}
	// Device matches if it supports at least one of the required ABIs.
	got := FilterDevices(devices, DeviceConstraints{RequiredABIs: []string{"arm64-v8a", "x86_64"}})
	if len(got) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(got))
	}
}
