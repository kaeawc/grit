package project

import (
	"os"
	"path/filepath"
	"regexp"
)

func parseSigningConfigs(prj *Project, body string, modDir string) map[string]SigningConfig {
	block, ok := extractNamedBlock(body, "signingConfigs")
	if !ok {
		return nil
	}
	valueVars := parseValueVariables(prj, body)
	out := map[string]SigningConfig{}
	re := regexp.MustCompile(`(?ms)(?:named\("([^"]+)"\)|create\("([^"]+)"\)|([A-Za-z0-9_]+))\s*\{`)
	indexes := re.FindAllStringSubmatchIndex(block, -1)
	for _, idx := range indexes {
		name := captureSubmatch(block, idx, 2)
		if name == "" {
			name = captureSubmatch(block, idx, 4)
		}
		if name == "" {
			name = captureSubmatch(block, idx, 6)
		}
		if name == "" {
			continue
		}
		openIdx := idx[1] - 1
		configBody, _, ok := extractBraceBodyAt(block, openIdx)
		if !ok {
			continue
		}
		cfg := SigningConfig{
			Name:          name,
			StorePassword: parseAssignment(configBody, `storePassword\s*=\s*"([^"]+)"`),
			KeyAlias:      parseAssignment(configBody, `keyAlias\s*=\s*"([^"]+)"`),
			KeyPassword:   parseAssignment(configBody, `keyPassword\s*=\s*"([^"]+)"`),
		}
		if storeFile := parseAssignment(configBody, `storeFile\s*=\s*file\("([^"]+)"\)`); storeFile != "" {
			cfg.StoreFile = filepath.Join(modDir, storeFile)
		}
		if cfg.StoreFile == "" {
			if varName := parseAssignment(configBody, `storeFile\s*=\s*([A-Za-z0-9_]+)`); varName != "" {
				cfg.StoreFile = resolvePotentialPath(prj, valueVars[varName])
			}
		}
		if cfg.StorePassword == "" {
			if varName := parseAssignment(configBody, `storePassword\s*=\s*([A-Za-z0-9_]+)`); varName != "" {
				cfg.StorePassword = valueVars[varName]
			}
		}
		if cfg.KeyAlias == "" {
			if varName := parseAssignment(configBody, `keyAlias\s*=\s*([A-Za-z0-9_]+)`); varName != "" {
				cfg.KeyAlias = valueVars[varName]
			}
		}
		if cfg.KeyPassword == "" {
			if varName := parseAssignment(configBody, `keyPassword\s*=\s*([A-Za-z0-9_]+)`); varName != "" {
				cfg.KeyPassword = valueVars[varName]
			}
		}
		out[name] = cfg
	}
	return out
}

func parseValueVariables(prj *Project, body string) map[string]string {
	out := map[string]string{}
	re := regexp.MustCompile(`(?ms)^\s*val\s+([A-Za-z0-9_]+):\s*String\?\s*=\s*System\.getenv\("([^"]+)"\)\s*\?:\s*findProperty\("([^"]+)"\)\s+as\s+String\?`)
	for _, match := range re.FindAllStringSubmatch(body, -1) {
		if value := firstNonEmpty(os.Getenv(match[2]), prj.GradleProperties[match[3]]); value != "" {
			out[match[1]] = value
		}
	}
	fileAliasRe := regexp.MustCompile(`(?ms)^\s*val\s+([A-Za-z0-9_]+):\s*File\?\s*=\s*([A-Za-z0-9_]+)\?\.let\s*\{`)
	for _, match := range fileAliasRe.FindAllStringSubmatch(body, -1) {
		if value := out[match[2]]; value != "" {
			out[match[1]] = value
		}
	}
	return out
}

func resolvePotentialPath(prj *Project, value string) string {
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(prj.RootDir, value)
}
