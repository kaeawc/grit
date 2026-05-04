package mavenlocalroot

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

type settings struct {
	LocalRepository string `xml:"localRepository"`
}

// Default returns the effective Maven local repository root based on the
// user's home directory and optional settings.xml override.
func Default() string {
	home := os.Getenv("HOME")
	if home == "" {
		return ""
	}
	confDir := mavenUserConfigDir(home)
	if confDir == "" {
		return ""
	}
	if override := settingsLocalRepository(confDir); override != "" {
		if filepath.IsAbs(override) {
			return filepath.Clean(override)
		}
		return filepath.Clean(filepath.Join(confDir, override))
	}
	return filepath.Join(home, ".m2", "repository")
}

func mavenUserConfigDir(home string) string {
	if override := strings.TrimSpace(os.Getenv("MAVEN_USER_HOME")); override != "" {
		return override
	}
	return filepath.Join(home, ".m2")
}

func settingsLocalRepository(confDir string) string {
	data, err := os.ReadFile(filepath.Join(confDir, "settings.xml"))
	if err != nil {
		return ""
	}
	var cfg settings
	if err := xml.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.LocalRepository)
}
