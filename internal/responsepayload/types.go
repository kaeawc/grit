package responsepayload

type UsageResult struct {
	Usage string `json:"usage"`
}

type CacheProbe struct {
	ActionID string `json:"actionId,omitempty"`
	State    string `json:"state"`
	Basis    string `json:"basis,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type CacheProbeRecord struct {
	ActionID string `json:"actionId,omitempty"`
	StepName string `json:"stepName,omitempty"`
	Order    int    `json:"order,omitempty"`
	State    string `json:"state"`
	Basis    string `json:"basis,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type PropertiesValues struct {
	Namespace                 string   `json:"namespace,omitempty"`
	ApplicationID             string   `json:"applicationId,omitempty"`
	VersionCode               string   `json:"versionCode,omitempty"`
	VersionName               string   `json:"versionName,omitempty"`
	CompileSDK                string   `json:"compileSdk,omitempty"`
	BuildToolsVersion         string   `json:"buildToolsVersion,omitempty"`
	MinSDK                    string   `json:"minSdk,omitempty"`
	TargetSDK                 string   `json:"targetSdk,omitempty"`
	TestInstrumentationRunner string   `json:"testInstrumentationRunner,omitempty"`
	UsesCompose               bool     `json:"usesCompose"`
	UsesMetro                 bool     `json:"usesMetro"`
	KotlinFreeCompilerArgs    []string `json:"kotlinFreeCompilerArgs,omitempty"`
	LintDisabledChecks        []string `json:"lintDisabledChecks,omitempty"`
	ConsumerProguardFiles     []string `json:"consumerProguardFiles,omitempty"`
	RequestedTasks            []string `json:"requestedTasks"`
}

type JavaToolchains struct {
	Java   JavaToolchainInfo   `json:"java"`
	Kotlin KotlinToolchainInfo `json:"kotlin"`
}

type JavaToolchainInfo struct {
	JavaHome string `json:"javaHome,omitempty"`
}

type KotlinToolchainInfo struct {
	Kotlinc string                 `json:"kotlinc,omitempty"`
	Plugins KotlinToolchainPlugins `json:"plugins"`
}

type KotlinToolchainPlugins struct {
	Compose       string `json:"compose,omitempty"`
	Serialization string `json:"serialization,omitempty"`
}
