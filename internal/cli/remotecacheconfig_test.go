package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".grit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRemoteCacheConfigFlagsTakePrecedence(t *testing.T) {
	t.Setenv(EnvRemoteCacheURL, "https://env.example/cache")
	t.Setenv(EnvRemoteCacheToken, "env-token")
	root := t.TempDir()
	writeConfig(t, root, `{"remoteCache":{"url":"https://file.example/cache","token":"file-token"}}`)

	cfg, err := ResolveRemoteCacheConfig(RemoteCacheFlags{
		URL:   "https://flag.example/cache",
		Token: "flag-token",
	}, root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.URL != "https://flag.example/cache" || cfg.Token != "flag-token" {
		t.Fatalf("flags should win: %+v", cfg)
	}
}

func TestResolveRemoteCacheConfigEnvBeatsFile(t *testing.T) {
	t.Setenv(EnvRemoteCacheURL, "https://env.example/cache")
	t.Setenv(EnvRemoteCacheToken, "env-token")
	root := t.TempDir()
	writeConfig(t, root, `{"remoteCache":{"url":"https://file.example/cache","token":"file-token"}}`)

	cfg, err := ResolveRemoteCacheConfig(RemoteCacheFlags{}, root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.URL != "https://env.example/cache" || cfg.Token != "env-token" {
		t.Fatalf("env should win when flags are empty: %+v", cfg)
	}
}

func TestResolveRemoteCacheConfigMixesEnvAndFile(t *testing.T) {
	t.Setenv(EnvRemoteCacheURL, "https://env.example/cache")
	t.Setenv(EnvRemoteCacheToken, "")
	root := t.TempDir()
	writeConfig(t, root, `{"remoteCache":{"url":"https://file.example/cache","token":"file-token"}}`)

	cfg, err := ResolveRemoteCacheConfig(RemoteCacheFlags{}, root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.URL != "https://env.example/cache" {
		t.Fatalf("URL: %q", cfg.URL)
	}
	if cfg.Token != "file-token" {
		t.Fatalf("Token: %q", cfg.Token)
	}
}

func TestResolveRemoteCacheConfigMissingFileIsNotAnError(t *testing.T) {
	t.Setenv(EnvRemoteCacheURL, "")
	t.Setenv(EnvRemoteCacheToken, "")
	cfg, err := ResolveRemoteCacheConfig(RemoteCacheFlags{}, t.TempDir())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.URL != "" || cfg.Token != "" {
		t.Fatalf("expected empty config, got %+v", cfg)
	}
}

func TestResolveRemoteCacheConfigMalformedFileIsAnError(t *testing.T) {
	t.Setenv(EnvRemoteCacheURL, "")
	t.Setenv(EnvRemoteCacheToken, "")
	root := t.TempDir()
	writeConfig(t, root, `not json`)
	_, err := ResolveRemoteCacheConfig(RemoteCacheFlags{}, root)
	if err == nil {
		t.Fatal("expected parse error for malformed config")
	}
}

func TestResolveRemoteCacheConfigEmptyRoot(t *testing.T) {
	t.Setenv(EnvRemoteCacheURL, "https://env.example/cache")
	t.Setenv(EnvRemoteCacheToken, "env-token")
	cfg, err := ResolveRemoteCacheConfig(RemoteCacheFlags{}, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.URL != "https://env.example/cache" || cfg.Token != "env-token" {
		t.Fatalf("expected env values when root is empty: %+v", cfg)
	}
}
