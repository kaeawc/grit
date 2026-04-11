package nativecompile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kaeawc/grit/internal/project"
)

func signAPK(ctx context.Context, mod *project.Module, variant project.BuildType, unsignedAPK, finalAPK string, stdout, stderr *os.File) error {
	signingName, signing := selectSigningConfig(mod, variant)
	if signingName == "" {
		if outputsNewerThanInputs(finalAPK, []string{unsignedAPK}) {
			return nil
		}
		if err := copyFile(unsignedAPK, finalAPK); err != nil {
			return err
		}
		return nil
	}
	if signing.StoreFile == "" {
		return fmt.Errorf("signing config %s missing storeFile", signingName)
	}
	sharedSignedAPK := sharedSignedAPKPath(unsignedAPK, signingName, signing)
	if restoreSharedSignedAPK(finalAPK, sharedSignedAPK) {
		return nil
	}
	if outputsNewerThanInputs(finalAPK, []string{unsignedAPK, signing.StoreFile}) {
		return nil
	}
	if err := copyFile(unsignedAPK, finalAPK); err != nil {
		return err
	}
	args := []string{
		"sign",
		"--ks", signing.StoreFile,
		"--ks-pass", "pass:" + signing.StorePassword,
		"--key-pass", "pass:" + signing.KeyPassword,
		"--ks-key-alias", signing.KeyAlias,
		finalAPK,
	}
	if err := runCmd(ctx, "apksigner", args, stdout, stderr); err != nil {
		return err
	}
	_ = saveSharedSignedAPK(finalAPK, sharedSignedAPK)
	return nil
}

func restoreSharedSignedAPKPreview(unsignedAPK, signingName string, signing project.SigningConfig, finalAPK string) bool {
	_ = finalAPK
	sharedSignedAPK := sharedSignedAPKPath(unsignedAPK, signingName, signing)
	return pathIsFile(sharedSignedAPK)
}

func selectSigningConfig(mod *project.Module, variant project.BuildType) (string, project.SigningConfig) {
	for _, candidate := range strings.Split(variant.SigningConfig, "|") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		signing, ok := mod.SigningConfigs[candidate]
		if ok && signing.StoreFile != "" {
			return candidate, signing
		}
		if candidate == "debug" {
			signing = defaultDebugSigningConfig()
			if signing.StoreFile != "" {
				return candidate, signing
			}
		}
	}
	return "", project.SigningConfig{}
}

func defaultDebugSigningConfig() project.SigningConfig {
	keystore := filepath.Join(os.Getenv("HOME"), ".android", "debug.keystore")
	if _, err := os.Stat(keystore); err != nil {
		return project.SigningConfig{}
	}
	return project.SigningConfig{
		Name:          "debug",
		StoreFile:     keystore,
		StorePassword: "android",
		KeyAlias:      "androiddebugkey",
		KeyPassword:   "android",
	}
}
