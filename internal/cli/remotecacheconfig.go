package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Environment variables consulted by ResolveRemoteCacheConfig.
const (
	EnvRemoteCacheURL   = "GRIT_REMOTE_CACHE_URL"
	EnvRemoteCacheToken = "GRIT_REMOTE_CACHE_TOKEN" // #nosec
)

// RemoteCacheConfig is the resolved URL/token pair used to build a
// remotecache.Client. Empty URL means "remote cache disabled".
type RemoteCacheConfig struct {
	URL   string
	Token string
}

// RemoteCacheFlags holds the values parsed from CLI --remote-cache-url and
// --remote-cache-token flags. Empty strings mean the flag was not provided.
type RemoteCacheFlags struct {
	URL   string
	Token string
}

// remoteCacheConfigFile is the on-disk shape persisted at
// <repoRoot>/.grit/config.json (top-level "remoteCache" key) for callers
// that prefer non-interactive configuration.
type remoteCacheConfigFile struct {
	RemoteCache *struct {
		URL   string `json:"url,omitempty"`
		Token string `json:"token,omitempty"`
	} `json:"remoteCache,omitempty"`
}

// ResolveRemoteCacheConfig merges remote-cache configuration from three
// sources, in descending precedence:
//
//  1. Explicit CLI flag values (RemoteCacheFlags)
//  2. Environment variables (GRIT_REMOTE_CACHE_URL / _TOKEN)
//  3. <repoRoot>/.grit/config.json under the "remoteCache" key
//
// repoRoot may be empty to skip the config-file lookup. Errors from
// reading or parsing the config file surface to the caller; missing
// config files are not errors.
func ResolveRemoteCacheConfig(flags RemoteCacheFlags, repoRoot string) (RemoteCacheConfig, error) {
	cfg := RemoteCacheConfig{
		URL:   strings.TrimSpace(flags.URL),
		Token: strings.TrimSpace(flags.Token),
	}

	if cfg.URL == "" {
		cfg.URL = strings.TrimSpace(os.Getenv(EnvRemoteCacheURL))
	}
	if cfg.Token == "" {
		cfg.Token = strings.TrimSpace(os.Getenv(EnvRemoteCacheToken))
	}

	if (cfg.URL == "" || cfg.Token == "") && repoRoot != "" {
		fileURL, fileToken, err := readRemoteCacheConfigFile(repoRoot)
		if err != nil {
			return RemoteCacheConfig{}, err
		}
		if cfg.URL == "" {
			cfg.URL = fileURL
		}
		if cfg.Token == "" {
			cfg.Token = fileToken
		}
	}

	return cfg, nil
}

func readRemoteCacheConfigFile(repoRoot string) (string, string, error) {
	path := filepath.Join(repoRoot, ".grit", "config.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("read %s: %w", path, err)
	}
	var parsed remoteCacheConfigFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", "", fmt.Errorf("parse %s: %w", path, err)
	}
	if parsed.RemoteCache == nil {
		return "", "", nil
	}
	return strings.TrimSpace(parsed.RemoteCache.URL), strings.TrimSpace(parsed.RemoteCache.Token), nil
}
