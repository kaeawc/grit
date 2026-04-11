package intellijtask

import (
	"fmt"
	"sort"
	"strings"
)

type ModuleKind string

const (
	ModuleKindUnknown            ModuleKind = ""
	ModuleKindAndroidApplication ModuleKind = "android-application"
	ModuleKindAndroidLibrary     ModuleKind = "android-library"
	ModuleKindJvmLibrary         ModuleKind = "jvm-library"
)

type Settings struct {
	ExternalProjectPath string     `json:"externalProjectPath,omitempty"`
	ModulePath          string     `json:"modulePath,omitempty"`
	ModuleKind          ModuleKind `json:"moduleKind,omitempty"`
	TaskNames           []string   `json:"taskNames,omitempty"`
	RequestedVariant    string     `json:"requestedVariant,omitempty"`
	VariantExplicit     bool       `json:"variantExplicit,omitempty"`
	DeviceSerial        string     `json:"deviceSerial,omitempty"`
	ScriptParameters    []string   `json:"scriptParameters,omitempty"`
	VMOptions           []string   `json:"vmOptions,omitempty"`
}

type Request struct {
	Settings Settings `json:"settings"`
}

type BuildRequest struct {
	ExternalProjectPath string     `json:"externalProjectPath,omitempty"`
	ModulePath          string     `json:"modulePath,omitempty"`
	ModuleKind          ModuleKind `json:"moduleKind,omitempty"`
	TaskName            string     `json:"taskName,omitempty"`
	Command             string     `json:"command,omitempty"`
	RequestedVariant    string     `json:"requestedVariant,omitempty"`
	VariantExplicit     bool       `json:"variantExplicit,omitempty"`
	DeviceSerial        string     `json:"deviceSerial,omitempty"`
	ScriptParameters    []string   `json:"scriptParameters,omitempty"`
	VMOptions           []string   `json:"vmOptions,omitempty"`
}

func (r Request) Resolve() ([]BuildRequest, error) {
	if len(r.Settings.TaskNames) == 0 {
		return nil, fmt.Errorf("taskNames is required")
	}
	var out []BuildRequest
	seen := map[string]struct{}{}
	for _, rawTask := range r.Settings.TaskNames {
		req, err := r.resolveTask(strings.TrimSpace(rawTask))
		if err != nil {
			return nil, err
		}
		key := requestKey(req)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, req)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ModulePath == out[j].ModulePath {
			if out[i].Command == out[j].Command {
				return out[i].TaskName < out[j].TaskName
			}
			return out[i].Command < out[j].Command
		}
		return out[i].ModulePath < out[j].ModulePath
	})
	return out, nil
}

func (r Request) resolveTask(rawTask string) (BuildRequest, error) {
	if rawTask == "" {
		return BuildRequest{}, fmt.Errorf("task name is required")
	}
	modulePath, taskName := splitQualifiedTask(rawTask)
	if modulePath == "" {
		modulePath = strings.TrimSpace(r.Settings.ModulePath)
	}
	if modulePath == "" {
		return BuildRequest{}, fmt.Errorf("module path is required for task %q", rawTask)
	}
	if r.Settings.ModulePath != "" && !sameModulePath(modulePath, r.Settings.ModulePath) {
		return BuildRequest{}, fmt.Errorf("task %q targets module %q, but settings modulePath is %q", rawTask, modulePath, r.Settings.ModulePath)
	}
	command, variant, explicit, err := normalizeTask(taskName, r.Settings.ModuleKind)
	if err != nil {
		return BuildRequest{}, err
	}
	requestedVariant := strings.TrimSpace(r.Settings.RequestedVariant)
	if requestedVariant != "" {
		if variant != "" && !strings.EqualFold(requestedVariant, variant) {
			return BuildRequest{}, fmt.Errorf("task %q implies variant %q, but requestedVariant is %q", rawTask, variant, requestedVariant)
		}
		variant = requestedVariant
		explicit = r.Settings.VariantExplicit || variant != ""
	}
	if variant == "" {
		variant = defaultVariantForKind(r.Settings.ModuleKind)
	}
	if command == "" {
		command = taskName
	}
	return BuildRequest{
		ExternalProjectPath: strings.TrimSpace(r.Settings.ExternalProjectPath),
		ModulePath:          modulePath,
		ModuleKind:          r.Settings.ModuleKind,
		TaskName:            rawTask,
		Command:             command,
		RequestedVariant:    variant,
		VariantExplicit:     explicit,
		DeviceSerial:        strings.TrimSpace(r.Settings.DeviceSerial),
		ScriptParameters:    append([]string(nil), r.Settings.ScriptParameters...),
		VMOptions:           append([]string(nil), r.Settings.VMOptions...),
	}, nil
}

func isAndroidKind(kind ModuleKind) bool {
	return kind == ModuleKindAndroidApplication || kind == ModuleKindAndroidLibrary
}

func splitQualifiedTask(task string) (string, string) {
	task = strings.TrimSpace(task)
	if task == "" {
		return "", ""
	}
	parts := strings.Split(task, ":")
	var filtered []string
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	if len(filtered) == 0 {
		return "", ""
	}
	if len(filtered) == 1 {
		return "", filtered[0]
	}
	modulePath := ":" + strings.Join(filtered[:len(filtered)-1], ":")
	return modulePath, filtered[len(filtered)-1]
}

func normalizeTask(task string, kind ModuleKind) (string, string, bool, error) {
	if isAndroidKind(kind) {
		if command, variant, explicit, ok := normalizeAndroidTask(task); ok {
			return command, variant, explicit, nil
		}
	}
	switch task {
	case "assemble":
		return "assemble", defaultVariantForKind(kind), false, nil
	case "build", "buildNeeded", "buildDependents":
		return task, defaultVariantForKind(kind), false, nil
	case "check", "test":
		return task, defaultVariantForKind(kind), false, nil
	case "assembleDebug":
		return "assemble-debug", "debug", true, nil
	case "assembleRelease":
		return "assemble-release", "release", true, nil
	case "compileDebugSources":
		return "compile-debug", "debug", true, nil
	case "compileReleaseSources":
		return "compile-release", "release", true, nil
	case "installDebug":
		return "install-debug", "debug", true, nil
	case "installRelease":
		return "install-release", "release", true, nil
	case "uninstallDebug":
		return "uninstallDebug", "debug", true, nil
	case "uninstallRelease":
		return "uninstallRelease", "release", true, nil
	case "uninstallAll":
		return "uninstallAll", defaultVariantForKind(kind), false, nil
	case "testDebugUnitTest":
		return "test-debug-unit", "debug", true, nil
	case "compileDebugUnitTestSources":
		return "compileDebugUnitTestSources", "debug", true, nil
	case "assembleUnitTest":
		return "assembleUnitTest", "debug", true, nil
	case "installDebugAndroidTest":
		return "install-android-tests", "debug", true, nil
	case "uninstallDebugAndroidTest":
		return "uninstall-android-tests", "debug", true, nil
	default:
		if strings.HasPrefix(task, "assemble") {
			suffix := strings.TrimPrefix(task, "assemble")
			if suffix != "" {
				return "assemble" + strings.ToLower(suffix[:1]) + suffix[1:], strings.ToLower(suffix[:1]) + suffix[1:], true, nil
			}
		}
		if strings.HasPrefix(task, "compile") && strings.HasSuffix(task, "Sources") {
			trimmed := strings.TrimSuffix(strings.TrimPrefix(task, "compile"), "Sources")
			if trimmed != "" {
				variant := strings.ToLower(trimmed[:1]) + trimmed[1:]
				return "compile-" + variant, variant, true, nil
			}
		}
		return "", "", false, fmt.Errorf("unsupported task %q", task)
	}
}

func normalizeAndroidTask(task string) (string, string, bool, bool) {
	switch task {
	case "assemble":
		return "assemble", defaultVariantForKind(ModuleKindAndroidApplication), false, true
	case "build", "buildNeeded", "buildDependents":
		return task, defaultVariantForKind(ModuleKindAndroidApplication), false, true
	case "check", "test":
		return task, defaultVariantForKind(ModuleKindAndroidApplication), false, true
	case "assembleDebug":
		return "assemble-debug", "debug", true, true
	case "assembleRelease":
		return "assemble-release", "release", true, true
	case "compileDebugSources":
		return "compile-debug", "debug", true, true
	case "compileReleaseSources":
		return "compile-release", "release", true, true
	case "installDebug":
		return "install-debug", "debug", true, true
	case "installRelease":
		return "install-release", "release", true, true
	case "uninstallDebug":
		return "uninstallDebug", "debug", true, true
	case "uninstallRelease":
		return "uninstallRelease", "release", true, true
	case "uninstallAll":
		return "uninstallAll", defaultVariantForKind(ModuleKindAndroidApplication), false, true
	case "testDebugUnitTest":
		return "test-debug-unit", "debug", true, true
	case "compileDebugUnitTestSources":
		return "compileDebugUnitTestSources", "debug", true, true
	case "assembleUnitTest":
		return "assembleUnitTest", "debug", true, true
	case "installDebugAndroidTest":
		return "install-android-tests", "debug", true, true
	case "uninstallDebugAndroidTest":
		return "uninstall-android-tests", "debug", true, true
	}
	if command, variant, explicit, ok := normalizeAndroidFlavoredTask(task); ok {
		return command, variant, explicit, true
	}
	return "", "", false, false
}

func normalizeAndroidFlavoredTask(task string) (string, string, bool, bool) {
	if command, variant, ok := normalizeAndroidVariantTask(task, "assemble", "", false); ok {
		return command, variant, true, true
	}
	if command, variant, ok := normalizeAndroidVariantTask(task, "install", "", false); ok {
		return command, variant, true, true
	}
	if command, variant, ok := normalizeAndroidVariantTask(task, "compile", "Sources", false); ok {
		return command, variant, true, true
	}
	if command, variant, ok := normalizeAndroidVariantTask(task, "test", "UnitTest", false); ok {
		return command, variant, true, true
	}
	if command, variant, ok := normalizeAndroidVariantTask(task, "compile", "UnitTestSources", true); ok {
		return command, variant, true, true
	}
	if _, variant, ok := normalizeAndroidVariantTask(task, "install", "AndroidTest", false); ok {
		return "install-android-tests", variant, true, true
	}
	if _, variant, ok := normalizeAndroidVariantTask(task, "uninstall", "AndroidTest", false); ok {
		return "uninstall-android-tests", variant, true, true
	}
	return "", "", false, false
}

func normalizeAndroidVariantTask(task, prefix, suffix string, stripUnitTest bool) (string, string, bool) {
	if !strings.HasPrefix(task, prefix) || !strings.HasSuffix(task, suffix) {
		return "", "", false
	}
	body := strings.TrimPrefix(task, prefix)
	body = strings.TrimSuffix(body, suffix)
	if stripUnitTest {
		body = strings.TrimSuffix(body, "UnitTest")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "", "", false
	}
	buildType := androidBuildTypeFromTaskBody(body)
	if buildType == "" {
		return "", "", false
	}
	variant := lowerFirst(body)
	switch prefix {
	case "assemble":
		return "assemble-" + buildType, variant, true
	case "install":
		return "install-" + buildType, variant, true
	case "uninstall":
		return "uninstall" + strings.ToUpper(buildType[:1]) + buildType[1:], variant, true
	case "compile":
		if suffix == "Sources" {
			return "compile-" + buildType, variant, true
		}
		if suffix == "UnitTestSources" {
			return "compileDebugUnitTestSources", variant, true
		}
	case "test":
		if suffix == "UnitTest" {
			return "test-debug-unit", variant, true
		}
	}
	return "", "", false
}

func androidBuildTypeFromTaskBody(body string) string {
	switch {
	case strings.HasSuffix(body, "Debug"):
		return "debug"
	case strings.HasSuffix(body, "Release"):
		return "release"
	default:
		return ""
	}
}

func lowerFirst(v string) string {
	if v == "" {
		return ""
	}
	if len(v) == 1 {
		return strings.ToLower(v)
	}
	return strings.ToLower(v[:1]) + v[1:]
}

func defaultVariantForKind(kind ModuleKind) string {
	if kind == ModuleKindJvmLibrary {
		return "main"
	}
	return "debug"
}

func requestKey(req BuildRequest) string {
	return strings.Join([]string{
		req.ExternalProjectPath,
		req.ModulePath,
		string(req.ModuleKind),
		req.TaskName,
		req.Command,
		req.RequestedVariant,
		fmt.Sprintf("%t", req.VariantExplicit),
		req.DeviceSerial,
		strings.Join(req.ScriptParameters, "\x00"),
		strings.Join(req.VMOptions, "\x00"),
	}, "\x1f")
}

func sameModulePath(left, right string) bool {
	return normalizeModulePath(left) == normalizeModulePath(right)
}

func normalizeModulePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = strings.TrimPrefix(path, ":")
	parts := strings.Split(path, ":")
	var filtered []string
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	return ":" + strings.Join(filtered, ":")
}
