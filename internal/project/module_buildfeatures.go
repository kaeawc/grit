package project

import "strings"

// parseBuildFeatures extracts the buildFeatures block from an Android
// build file and returns a populated BuildFeatures struct. It recognises
// the four standard keys: compose, viewBinding, dataBinding, buildConfig.
func parseBuildFeatures(body string) BuildFeatures {
	block, ok := extractNamedBlock(body, "buildFeatures")
	if !ok {
		return BuildFeatures{}
	}
	return BuildFeatures{
		Compose:     strings.Contains(block, "compose = true"),
		ViewBinding: strings.Contains(block, "viewBinding = true"),
		DataBinding: strings.Contains(block, "dataBinding = true"),
		BuildConfig: strings.Contains(block, "buildConfig = true"),
	}
}
