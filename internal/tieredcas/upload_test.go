package tieredcas

import "testing"

func TestUploadPolicyDeniesByDefault(t *testing.T) {
	var p UploadPolicy
	if p.ShouldUpload("compile", 0, 1<<20) {
		t.Fatal("zero-value policy should deny everything")
	}
}

func TestUploadPolicyKindGate(t *testing.T) {
	p := UploadPolicy{AllowedKinds: []string{"compile", "dex"}}
	if !p.ShouldUpload("compile", 0, 0) {
		t.Fatal("compile should be allowed")
	}
	if !p.ShouldUpload("dex", 0, 0) {
		t.Fatal("dex should be allowed")
	}
	if p.ShouldUpload("lint", 0, 0) {
		t.Fatal("lint should be denied")
	}
}

func TestUploadPolicyWildcardKind(t *testing.T) {
	p := UploadPolicy{AllowedKinds: []string{"*"}}
	if !p.ShouldUpload("anything", 0, 0) {
		t.Fatal("wildcard should accept any kind")
	}
}

func TestUploadPolicyMinTier(t *testing.T) {
	p := UploadPolicy{AllowedKinds: []string{"*"}, MinTier: 1}
	if p.ShouldUpload("compile", 0, 0) {
		t.Fatal("tier 0 should be skipped when MinTier=1")
	}
	if !p.ShouldUpload("compile", 1, 0) {
		t.Fatal("tier 1 should pass MinTier=1")
	}
	if !p.ShouldUpload("compile", 2, 0) {
		t.Fatal("tier 2 should pass MinTier=1")
	}
}

func TestUploadPolicyMinResultSize(t *testing.T) {
	p := UploadPolicy{AllowedKinds: []string{"*"}, MinResultSize: 1024}
	if p.ShouldUpload("compile", 0, 1023) {
		t.Fatal("under-size result should be denied")
	}
	if !p.ShouldUpload("compile", 0, 1024) {
		t.Fatal("at-threshold result should be allowed")
	}
	if !p.ShouldUpload("compile", 0, 4096) {
		t.Fatal("oversize result should be allowed")
	}
}

func TestUploadPolicyAllGatesCombine(t *testing.T) {
	p := UploadPolicy{
		AllowedKinds:  []string{"compile"},
		MinTier:       1,
		MinResultSize: 1024,
	}
	cases := []struct {
		name       string
		kind       string
		tier       int
		size       int64
		wantUpload bool
	}{
		{"all gates pass", "compile", 1, 1024, true},
		{"wrong kind", "lint", 1, 1024, false},
		{"too-low tier", "compile", 0, 1024, false},
		{"too-small result", "compile", 1, 100, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := p.ShouldUpload(tc.kind, tc.tier, tc.size)
			if got != tc.wantUpload {
				t.Errorf("ShouldUpload(%q, %d, %d) = %v, want %v", tc.kind, tc.tier, tc.size, got, tc.wantUpload)
			}
		})
	}
}
