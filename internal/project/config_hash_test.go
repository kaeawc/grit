package project

import "testing"

func TestConfigHashDeterministic(t *testing.T) {
	t.Parallel()
	v := ResolvedVariant{
		ApplicationID: "com.example.app",
		MinSDK:        "24",
		TargetSDK:     "34",
		Coordinate: VariantCoordinate{
			BuildType: "debug",
			Flavors:   []string{"free"},
		},
	}
	first := v.ConfigHash()
	second := v.ConfigHash()
	if first != second {
		t.Fatalf("ConfigHash is not deterministic: %q != %q", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("expected 64-char hex string, got %d chars: %q", len(first), first)
	}
}

func TestConfigHashSensitiveToMinSDK(t *testing.T) {
	t.Parallel()
	base := ResolvedVariant{
		ApplicationID: "com.example.app",
		MinSDK:        "24",
		TargetSDK:     "34",
		Coordinate:    VariantCoordinate{BuildType: "debug"},
	}
	changed := base
	changed.MinSDK = "21"
	if base.ConfigHash() == changed.ConfigHash() {
		t.Fatal("ConfigHash should change when MinSDK changes")
	}
}

func TestConfigHashSensitiveToApplicationID(t *testing.T) {
	t.Parallel()
	base := ResolvedVariant{
		ApplicationID: "com.example.app",
		MinSDK:        "24",
		Coordinate:    VariantCoordinate{BuildType: "debug"},
	}
	changed := base
	changed.ApplicationID = "com.example.app.staging"
	if base.ConfigHash() == changed.ConfigHash() {
		t.Fatal("ConfigHash should change when ApplicationID changes")
	}
}

func TestConfigHashSensitiveToOptimization(t *testing.T) {
	t.Parallel()
	base := ResolvedVariant{
		Optimization: VariantOptimization{MinifyEnabled: false},
		Coordinate:   VariantCoordinate{BuildType: "debug"},
	}
	changed := base
	changed.Optimization.MinifyEnabled = true
	if base.ConfigHash() == changed.ConfigHash() {
		t.Fatal("ConfigHash should change when optimization flags change")
	}
}

func TestConfigHashSensitiveToBuildType(t *testing.T) {
	t.Parallel()
	debug := ResolvedVariant{
		Coordinate: VariantCoordinate{BuildType: "debug"},
	}
	release := ResolvedVariant{
		Coordinate: VariantCoordinate{BuildType: "release"},
	}
	if debug.ConfigHash() == release.ConfigHash() {
		t.Fatal("ConfigHash should differ between build types")
	}
}

func TestConfigHashSensitiveToFlavors(t *testing.T) {
	t.Parallel()
	free := ResolvedVariant{
		Coordinate: VariantCoordinate{BuildType: "debug", Flavors: []string{"free"}},
	}
	paid := ResolvedVariant{
		Coordinate: VariantCoordinate{BuildType: "debug", Flavors: []string{"paid"}},
	}
	if free.ConfigHash() == paid.ConfigHash() {
		t.Fatal("ConfigHash should differ between flavors")
	}
}

func TestConfigHashSensitiveToProguardFiles(t *testing.T) {
	t.Parallel()
	base := ResolvedVariant{
		Coordinate: VariantCoordinate{BuildType: "release"},
	}
	withProguard := ResolvedVariant{
		Coordinate:    VariantCoordinate{BuildType: "release"},
		ProguardFiles: []string{"proguard-rules.pro"},
	}
	if base.ConfigHash() == withProguard.ConfigHash() {
		t.Fatal("ConfigHash should change when proguard files change")
	}
}

func TestConfigHashFlavorOrderIndependent(t *testing.T) {
	t.Parallel()
	a := ResolvedVariant{
		Coordinate: VariantCoordinate{BuildType: "debug", Flavors: []string{"free", "arm"}},
	}
	b := ResolvedVariant{
		Coordinate: VariantCoordinate{BuildType: "debug", Flavors: []string{"arm", "free"}},
	}
	if a.ConfigHash() != b.ConfigHash() {
		t.Fatal("ConfigHash should be order-independent for flavors")
	}
}
