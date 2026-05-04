package nativecompile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/project"
)

func TestSignAABCopiesUnsignedWhenNoSigningConfig(t *testing.T) {
	tmp := t.TempDir()
	unsigned := filepath.Join(tmp, "app-unsigned.aab")
	signed := filepath.Join(tmp, "app.aab")
	if err := os.WriteFile(unsigned, []byte("fake-aab"), 0644); err != nil {
		t.Fatal(err)
	}

	mod := &project.Module{SigningConfigs: map[string]project.SigningConfig{}}
	variant := project.BuildType{Name: "release"}

	err := signAAB(t.Context(), mod, variant, unsigned, signed, os.Stdout, os.Stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(signed)
	if err != nil {
		t.Fatalf("signed AAB not created: %v", err)
	}
	if string(data) != "fake-aab" {
		t.Fatalf("signed AAB content mismatch: got %q", string(data))
	}
}

func TestSignAABFailsWhenJarsignerMissing(t *testing.T) {
	tmp := t.TempDir()
	unsigned := filepath.Join(tmp, "app-unsigned.aab")
	signed := filepath.Join(tmp, "app.aab")
	keystore := filepath.Join(tmp, "release.jks")
	if err := os.WriteFile(unsigned, []byte("fake-aab"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keystore, []byte("fake-keystore"), 0644); err != nil {
		t.Fatal(err)
	}

	mod := &project.Module{
		SigningConfigs: map[string]project.SigningConfig{
			"release": {Name: "release", StoreFile: keystore, KeyAlias: "key0", StorePassword: "pass", KeyPassword: "pass"},
		},
	}
	variant := project.BuildType{Name: "release", SigningConfig: "release"}

	// Set PATH to empty so jarsigner can't be found.
	t.Setenv("PATH", tmp)
	err := signAAB(t.Context(), mod, variant, unsigned, signed, os.Stdout, os.Stderr)
	if err == nil {
		t.Fatal("expected error when jarsigner is not available")
	}
	if !strings.Contains(err.Error(), "jarsigner") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSignAABSkipsWhenOutputNewer(t *testing.T) {
	tmp := t.TempDir()
	unsigned := filepath.Join(tmp, "app-unsigned.aab")
	signed := filepath.Join(tmp, "app.aab")

	// Create unsigned first, then signed — signed is newer.
	if err := os.WriteFile(unsigned, []byte("unsigned"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(signed, []byte("already-signed"), 0644); err != nil {
		t.Fatal(err)
	}

	mod := &project.Module{SigningConfigs: map[string]project.SigningConfig{}}
	variant := project.BuildType{Name: "release"}

	err := signAAB(t.Context(), mod, variant, unsigned, signed, os.Stdout, os.Stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not overwrite — content should remain "already-signed".
	data, _ := os.ReadFile(signed)
	if string(data) != "already-signed" {
		t.Fatalf("expected signed AAB to be unchanged, got %q", string(data))
	}
}

func TestSharedSignedAABPathDiffersFromAPK(t *testing.T) {
	signing := project.SigningConfig{
		StoreFile: "/tmp/keystore.jks",
		KeyAlias:  "key0",
	}
	aabPath := sharedSignedAABPath("/tmp/app.aab", "release", signing)
	apkPath := sharedSignedAPKPath("/tmp/app.apk", "release", signing)

	if aabPath == apkPath {
		t.Fatal("AAB and APK signed cache paths should differ")
	}
	if !strings.Contains(aabPath, "aab") {
		t.Fatalf("AAB cache path should contain 'aab': %s", aabPath)
	}
	if !strings.HasSuffix(aabPath, "app.aab") {
		t.Fatalf("AAB cache path should end with app.aab: %s", aabPath)
	}
}

func TestSaveAndRestoreSharedSignedAAB(t *testing.T) {
	tmp := t.TempDir()

	// Simulate a signed AAB.
	signedAAB := filepath.Join(tmp, "app.aab")
	if err := os.WriteFile(signedAAB, []byte("signed-content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Save to cache.
	cacheAAB := filepath.Join(tmp, "cache", "aab", "signed", "abc123", "app.aab")
	if err := saveSharedSignedAAB(signedAAB, cacheAAB); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Verify cache file exists.
	if !pathIsFile(cacheAAB) {
		t.Fatal("cached AAB not found")
	}

	// Restore to a new location.
	restored := filepath.Join(tmp, "restored.aab")
	if !restoreSharedSignedAAB(restored, cacheAAB) {
		t.Fatal("restore returned false")
	}
	data, _ := os.ReadFile(restored)
	if string(data) != "signed-content" {
		t.Fatalf("restored content mismatch: got %q", string(data))
	}
}

func TestRestoreSharedSignedAABReturnsFalseWhenMissing(t *testing.T) {
	if restoreSharedSignedAAB("/tmp/out.aab", "/nonexistent/app.aab") {
		t.Fatal("expected false for missing cache")
	}
}

func TestRestoreSharedSignedAABPreviewReturnsTrueWhenCacheExists(t *testing.T) {
	tmp := t.TempDir()

	// Create a fake unsigned AAB and keystore so the cache path can be computed.
	unsigned := filepath.Join(tmp, "app-unsigned.aab")
	keystore := filepath.Join(tmp, "release.jks")
	if err := os.WriteFile(unsigned, []byte("fake-aab"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keystore, []byte("fake-keystore"), 0644); err != nil {
		t.Fatal(err)
	}

	signing := project.SigningConfig{
		StoreFile: keystore,
		KeyAlias:  "key0",
	}

	// Before cache exists, preview should return false.
	if restoreSharedSignedAABPreview(unsigned, "release", signing) {
		t.Fatal("expected false before cache is populated")
	}

	// Create the signed AAB in the shared cache location.
	sharedPath := sharedSignedAABPath(unsigned, "release", signing)
	if err := os.MkdirAll(filepath.Dir(sharedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sharedPath, []byte("signed-aab"), 0644); err != nil {
		t.Fatal(err)
	}

	// Preview should now return true.
	if !restoreSharedSignedAABPreview(unsigned, "release", signing) {
		t.Fatal("expected true when shared cache exists")
	}
}

func TestRestoreSharedSignedAABPreviewDoesNotCopyFile(t *testing.T) {
	tmp := t.TempDir()

	unsigned := filepath.Join(tmp, "app-unsigned.aab")
	keystore := filepath.Join(tmp, "release.jks")
	if err := os.WriteFile(unsigned, []byte("fake-aab"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keystore, []byte("fake-keystore"), 0644); err != nil {
		t.Fatal(err)
	}

	signing := project.SigningConfig{
		StoreFile: keystore,
		KeyAlias:  "key0",
	}

	// Populate the shared cache.
	sharedPath := sharedSignedAABPath(unsigned, "release", signing)
	if err := os.MkdirAll(filepath.Dir(sharedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sharedPath, []byte("signed-aab"), 0644); err != nil {
		t.Fatal(err)
	}

	// Call preview — it should NOT create any output file.
	finalAAB := filepath.Join(tmp, "app.aab")
	_ = restoreSharedSignedAABPreview(unsigned, "release", signing)
	if pathIsFile(finalAAB) {
		t.Fatal("preview should not copy the cached file to the output path")
	}
}
