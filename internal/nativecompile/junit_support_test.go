package nativecompile

import "testing"

func TestJUnitJupiterVersionForPlatform(t *testing.T) {
	t.Parallel()

	tests := []struct {
		platform string
		want     string
	}{
		{platform: "1.12.2", want: "5.12.2"},
		{platform: "1.9.2", want: "5.9.2"},
		{platform: "6.0.3", want: "6.0.3"},
		{platform: "bad", want: ""},
	}
	for _, tt := range tests {
		if got := junitJupiterVersionForPlatform(tt.platform); got != tt.want {
			t.Fatalf("junitJupiterVersionForPlatform(%q) = %q, want %q", tt.platform, got, tt.want)
		}
	}
}
