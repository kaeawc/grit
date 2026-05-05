package catalog

import "testing"

func TestResolveBundleReturnsCopy(t *testing.T) {
	cat := &Catalog{
		Bundles: map[string][]string{
			"unit-test": {"junit", "mockk"},
		},
	}

	bundle, err := cat.ResolveBundle("unit.test")
	if err != nil {
		t.Fatal(err)
	}
	bundle[0] = "mutated"
	mutated := append(bundle, "extra")
	if got, want := len(mutated), 3; got != want {
		t.Fatalf("mutated bundle length = %d, want %d", got, want)
	}

	fresh, err := cat.ResolveBundle("unit.test")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(fresh), 2; got != want {
		t.Fatalf("fresh bundle length = %d, want %d", got, want)
	}
	if got, want := fresh[0], "junit"; got != want {
		t.Fatalf("fresh bundle first ref = %q, want %q", got, want)
	}
	if got, want := fresh[1], "mockk"; got != want {
		t.Fatalf("fresh bundle second ref = %q, want %q", got, want)
	}
}
