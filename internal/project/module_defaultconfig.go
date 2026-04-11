package project

func parseDefaultConfig(prj *Project, body string) DefaultConfig {
	block, ok := extractNamedBlock(body, "defaultConfig")
	if !ok {
		return DefaultConfig{}
	}
	return DefaultConfig{
		ApplicationID:     parseAssignment(block, `applicationId\s*=\s*"([^"]+)"`),
		VersionCode:       resolveCatalogValue(prj, firstNonEmpty(parseAssignment(block, `versionCode\s*=\s*([0-9]+)`))),
		VersionName:       parseAssignment(block, `versionName\s*=\s*"([^"]+)"`),
		MinSDK:            resolveCatalogValue(prj, firstNonEmpty(parseAssignment(block, `minSdk\s*=\s*(\d+)`))),
		TargetSDK:         resolveCatalogValue(prj, firstNonEmpty(parseAssignment(block, `targetSdk\s*=\s*(\d+)`))),
		MissingDimensions: parseMissingDimensionStrategies(block),
	}
}
