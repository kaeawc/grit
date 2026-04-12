package project

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
)

// ConfigHash computes a deterministic SHA-256 hash over the resolved
// configuration values that affect build outputs. When a configuration
// value changes (e.g. minSdk, applicationId, optimization flags) but
// the variant name stays the same, this hash changes and invalidates
// dependent caches.
func (v ResolvedVariant) ConfigHash() string {
	h := sha256.New()

	// Write each key-value pair in a fixed, sorted order so the hash is
	// deterministic regardless of struct field ordering or future additions.
	pairs := []struct{ k, v string }{
		{"applicationId", v.ApplicationID},
		{"applicationIdSuffix", v.ApplicationIDSuffix},
		{"buildToolsVersion", v.BuildToolsVersion},
		{"buildType", v.Coordinate.BuildType},
		{"compileSdk", v.CompileSDK},
		{"debuggable", strconv.FormatBool(v.Debuggable)},
		{"dexMode", v.DexMode},
		{"minSdk", v.MinSDK},
		{"minifyEnabled", strconv.FormatBool(v.MinifyEnabled)},
		{"namespace", v.Namespace},
		{"optimization.minifyEnabled", strconv.FormatBool(v.Optimization.MinifyEnabled)},
		{"optimization.shrinkResources", strconv.FormatBool(v.Optimization.ShrinkResources)},
		{"shrinkResources", strconv.FormatBool(v.ShrinkResources)},
		{"signingConfig", v.SigningConfig},
		{"targetSdk", v.TargetSDK},
		{"testInstrumentationRunner", v.TestInstrumentationRunner},
		{"versionCode", v.VersionCode},
		{"versionName", v.VersionName},
		{"versionNameSuffix", v.VersionNameSuffix},
	}

	for _, p := range pairs {
		h.Write([]byte(p.k))
		h.Write([]byte{0})
		h.Write([]byte(p.v))
		h.Write([]byte{0})
	}

	// Flavors — sorted for determinism.
	flavors := append([]string(nil), v.Coordinate.Flavors...)
	sort.Strings(flavors)
	h.Write([]byte("flavors"))
	h.Write([]byte{0})
	for _, f := range flavors {
		h.Write([]byte(f))
		h.Write([]byte{0})
	}

	// Proguard files — order matters, preserve it.
	h.Write([]byte("proguardFiles"))
	h.Write([]byte{0})
	for _, p := range v.ProguardFiles {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}

	h.Write([]byte("consumerProguardFiles"))
	h.Write([]byte{0})
	for _, p := range v.ConsumerProguardFiles {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}

	return hex.EncodeToString(h.Sum(nil))
}
