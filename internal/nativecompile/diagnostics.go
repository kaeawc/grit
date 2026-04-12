package nativecompile

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/kaeawc/grit/internal/tooldiag"
)

var (
	diagWithColumn       = regexp.MustCompile(`(?i)^(.+?):(\d+):(\d+):\s*(error|warning|info|note|information|informational):\s*(.+)$`)
	diagWithoutCol       = regexp.MustCompile(`(?i)^(.+?):(\d+):\s*(error|warning|info|note|information|informational):\s*(.+)$`)
	kotlinPrefix         = regexp.MustCompile(`^(e|w|i|n):\s*(.+?):\s*\((\d+),\s*(\d+)\):\s*(.+)$`)
	bracketCode          = regexp.MustCompile(`^\[([A-Za-z0-9_.-]+)\]\s*(.+)$`)
	failureBracketCode   = regexp.MustCompile(`(?i)\bfailure\s*\[([A-Za-z0-9_.-]+)(?::\s*(.+?))?\]`)
	javaExceptionLine    = regexp.MustCompile(`(?i)^(?:exception in thread ".+" .+|caused by:\s+.+|[A-Za-z0-9_$.]+(?:exception|error)(?::|\b).*)$`)
	dependencyCoordinate = regexp.MustCompile(`\b([A-Za-z0-9_.-]+:[A-Za-z0-9_.-]+:[A-Za-z0-9][A-Za-z0-9+_.-]*)\b`)
)

func recordToolDiagnostics(ctx context.Context, tool string, streams ...string) {
	streamNames := []string{"stderr", "stdout"}
	var records []tooldiag.Record
	for i, stream := range streams {
		streamName := "output"
		if i < len(streamNames) {
			streamName = streamNames[i]
		}
		records = append(records, parseToolDiagnostics(tool, streamName, stream)...)
	}
	tooldiag.RecordAll(ctx, records)
}

func parseToolDiagnostics(tool, streamName, stream string) []tooldiag.Record {
	var records []tooldiag.Record
	for _, line := range strings.Split(stream, "\n") {
		record, ok := parseToolDiagnosticLine(tool, streamName, line)
		if !ok {
			continue
		}
		records = append(records, record)
	}
	return records
}

func parseToolDiagnosticLine(tool, streamName, line string) (tooldiag.Record, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "TRACE ") || strings.HasPrefix(line, "grit d8 ") {
		return tooldiag.Record{}, false
	}
	if matches := diagWithColumn.FindStringSubmatch(line); len(matches) == 6 {
		severity := normalizeSeverity(matches[4])
		code, category := classifyDiagnostic(tool, severity, matches[5])
		return tooldiag.Record{
			Tool:              tool,
			Severity:          severity,
			Code:              code,
			Category:          category,
			Message:           strings.TrimSpace(matches[5]),
			File:              strings.TrimSpace(matches[1]),
			Line:              parseInt(matches[2]),
			Column:            parseInt(matches[3]),
			SourceKind:        "tool-emitted",
			Stream:            streamName,
			RelatedDependency: extractRelatedDependency(matches[5]),
		}, true
	}
	if matches := diagWithoutCol.FindStringSubmatch(line); len(matches) == 5 {
		severity := normalizeSeverity(matches[3])
		code, category := classifyDiagnostic(tool, severity, matches[4])
		return tooldiag.Record{
			Tool:              tool,
			Severity:          severity,
			Code:              code,
			Category:          category,
			Message:           strings.TrimSpace(matches[4]),
			File:              strings.TrimSpace(matches[1]),
			Line:              parseInt(matches[2]),
			SourceKind:        "tool-emitted",
			Stream:            streamName,
			RelatedDependency: extractRelatedDependency(matches[4]),
		}, true
	}
	if matches := kotlinPrefix.FindStringSubmatch(line); len(matches) == 6 {
		severity := severityFromKotlinPrefix(matches[1])
		code, category := classifyDiagnostic(tool, severity, matches[5])
		return tooldiag.Record{
			Tool:              tool,
			Severity:          severity,
			Code:              code,
			Category:          category,
			Message:           strings.TrimSpace(matches[5]),
			File:              strings.TrimPrefix(strings.TrimSpace(matches[2]), "file://"),
			Line:              parseInt(matches[3]),
			Column:            parseInt(matches[4]),
			SourceKind:        "tool-emitted",
			Stream:            streamName,
			RelatedDependency: extractRelatedDependency(matches[5]),
		}, true
	}
	severity := inferredSeverity(line)
	if severity == "" {
		severity = inferredSeverityForTool(tool, line)
	}
	if severity == "" {
		return tooldiag.Record{}, false
	}
	code, category := classifyDiagnostic(tool, severity, line)
	return tooldiag.Record{
		Tool:              tool,
		Severity:          severity,
		Code:              code,
		Category:          category,
		Message:           line,
		SourceKind:        "tool-emitted",
		Stream:            streamName,
		RelatedDependency: extractRelatedDependency(line),
	}, true
}

func severityFromKotlinPrefix(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "w":
		return "warning"
	case "i", "n":
		return "info"
	default:
		return "error"
	}
}

func normalizeSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "warning":
		return "warning"
	case "info", "note", "information", "informational":
		return "info"
	default:
		return "error"
	}
}

func inferredSeverity(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, " error:") || strings.HasPrefix(lower, "error:") || strings.Contains(lower, " failed") || strings.HasPrefix(lower, "failed "):
		return "error"
	case strings.Contains(lower, "failure [install_failed") || strings.Contains(lower, "failure [delete_failed") || strings.Contains(lower, "failure [uninstall_failed"):
		return "error"
	case strings.HasPrefix(lower, "unknown package:"):
		return "error"
	case isJavaRuntimeExceptionLine(line):
		return "error"
	case strings.Contains(lower, " warning:") || strings.HasPrefix(lower, "warning:"):
		return "warning"
	case strings.Contains(lower, " note:") || strings.HasPrefix(lower, "note:"):
		return "info"
	default:
		return ""
	}
}

func inferredSeverityForTool(tool, line string) string {
	lower := strings.ToLower(line)
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "jarsigner":
		switch {
		case strings.Contains(lower, "unable to open jar file"),
			strings.Contains(lower, "unable to sign jar file"),
			strings.Contains(lower, "failed to sign"),
			strings.Contains(lower, "jar signing failed"):
			return "error"
		case strings.Contains(lower, "self-signed"),
			strings.Contains(lower, "not timestamped"),
			strings.Contains(lower, "will be treated as unsigned"),
			strings.Contains(lower, "certificate chain not validated"):
			return "warning"
		}
	case "zipalign":
		switch {
		case strings.Contains(lower, "can't open"),
			strings.Contains(lower, "cannot open"),
			strings.Contains(lower, "failed to open"),
			strings.Contains(lower, "failed to align"),
			strings.Contains(lower, "not a zip file"),
			strings.Contains(lower, "zipalign failed"):
			return "error"
		case strings.Contains(lower, "not aligned"),
			strings.Contains(lower, "alignment warning"),
			strings.Contains(lower, "unaligned"):
			return "warning"
		}
	case "lint":
		switch {
		case strings.Contains(lower, "error:") || strings.HasPrefix(lower, "error:"):
			return "error"
		case strings.Contains(lower, "warning:") || strings.HasPrefix(lower, "warning:"):
			return "warning"
		case strings.Contains(lower, "information:") || strings.Contains(lower, "informational:"):
			return "info"
		}
	case "manifest-merger":
		switch {
		case strings.Contains(lower, "merge failed") || strings.Contains(lower, "merging failed"),
			strings.Contains(lower, "conflict") && strings.Contains(lower, "attribute"):
			return "error"
		case strings.Contains(lower, "suggestion:") || strings.Contains(lower, "tools:replace") || strings.Contains(lower, "tools:node"):
			return "warning"
		}
	}
	return ""
}

func classifyDiagnostic(tool, severity, message string) (string, string) {
	normalizedTool := strings.ToLower(strings.TrimSpace(tool))
	lower := strings.ToLower(strings.TrimSpace(message))
	switch normalizedTool {
	case "adb":
		switch {
		case strings.Contains(lower, "install_failed"):
			return "adb_install_failed", "device-install"
		case strings.Contains(lower, "delete_failed") || strings.Contains(lower, "uninstall_failed"):
			return "adb_uninstall_failed", "device-install"
		case strings.Contains(lower, "install failed"):
			return "adb_install_failed", "device-install"
		case strings.Contains(lower, "delete failed") || strings.Contains(lower, "uninstall failed") || strings.HasPrefix(lower, "unknown package:"):
			return "adb_uninstall_failed", "device-install"
		}
	case "java":
		switch {
		case strings.Contains(lower, "error") && !strings.Contains(lower, "exception"):
			return "java_error", "runtime"
		case isJavaRuntimeExceptionLine(message) || strings.Contains(lower, "exception"):
			return "java_exception", "runtime"
		case strings.Contains(lower, "error"):
			return "java_error", "runtime"
		}
	}
	if code, _, ok := extractBracketCode(message); ok {
		sanitized := sanitizeDiagnosticToken(code)
		category := sanitized
		if normalizedTool == "lint" {
			category = classifyLintIssueID(sanitized)
		}
		return normalizedTool + "_" + sanitized, category
	}
	switch normalizedTool {
	case "kotlinc":
		switch {
		case strings.Contains(lower, "unresolved reference"):
			return "kotlinc_unresolved_reference", "symbol-resolution"
		case strings.Contains(lower, "type mismatch"):
			return "kotlinc_type_mismatch", "type-checking"
		case strings.Contains(lower, "is never used"):
			return "kotlinc_unused_symbol", "unused-code"
		case strings.Contains(lower, "redeclaration"):
			return "kotlinc_redeclaration", "symbol-resolution"
		}
	case "javac":
		switch {
		case strings.Contains(lower, "cannot find symbol"):
			return "javac_cannot_find_symbol", "symbol-resolution"
		case strings.Contains(lower, "package ") && strings.Contains(lower, " does not exist"):
			return "javac_missing_package", "classpath"
		case strings.Contains(lower, "incompatible types"):
			return "javac_incompatible_types", "type-checking"
		case strings.Contains(lower, "has been deprecated"),
			strings.Contains(lower, "uses or overrides a deprecated api"),
			strings.Contains(lower, "-xlint:deprecation"):
			return "javac_deprecated_api", "deprecation"
		case strings.Contains(lower, "uses unchecked or unsafe operations"),
			strings.Contains(lower, "-xlint:unchecked"):
			return "javac_unchecked_operations", "unchecked"
		}
	case "aapt2":
		switch {
		case strings.Contains(lower, "failed linking references"):
			return "aapt2_link_failed", "resource-linking"
		case strings.Contains(lower, "failed to compile file"),
			strings.Contains(lower, "failed compiling"),
			strings.Contains(lower, "resource compilation failed"):
			return "aapt2_compile_failed", "resource-compilation"
		case strings.Contains(lower, "duplicate resource"):
			return "aapt2_duplicate_resource", "resources"
		case strings.Contains(lower, "failed parsing xml"),
			strings.Contains(lower, "xml file line") && strings.Contains(lower, "error"):
			return "aapt2_xml_parse_failed", "resources"
		case strings.Contains(lower, "resource entry") && strings.Contains(lower, "invalid character"),
			strings.Contains(lower, "is not a valid resource name"):
			return "aapt2_invalid_resource_name", "resources"
		case strings.Contains(lower, "resource ") && strings.Contains(lower, " not found"):
			return "aapt2_missing_resource", "resources"
		}
	case "d8":
		switch {
		case strings.Contains(lower, "duplicate class"):
			return "d8_duplicate_class", "dexing"
		case strings.Contains(lower, "missing class"):
			return "d8_missing_class", "classpath"
		}
	case "r8":
		switch {
		case strings.Contains(lower, "missing class"):
			return "r8_missing_class", "shrinking"
		case strings.Contains(lower, "duplicate class"):
			return "r8_duplicate_class", "shrinking"
		case strings.Contains(lower, "missing keep"):
			return "r8_missing_keep_rule", "shrinking"
		}
	case "apksigner":
		switch {
		case strings.Contains(lower, "failed to load signer"),
			strings.Contains(lower, "failed to sign"),
			strings.Contains(lower, "keystore was tampered with"),
			strings.Contains(lower, "password was incorrect"),
			strings.Contains(lower, "failed to read key"),
			strings.Contains(lower, "private key"),
			strings.Contains(lower, "certificate"):
			return "apksigner_sign_failed", "signing"
		case strings.Contains(lower, "not protected by this signature"),
			strings.Contains(lower, "signer #") && strings.Contains(lower, "warning"):
			return "apksigner_unprotected_entry", "signing"
		}
	case "jarsigner":
		switch {
		case strings.Contains(lower, "unable to open jar file"),
			strings.Contains(lower, "unable to sign jar file"),
			strings.Contains(lower, "jar signing failed"),
			strings.Contains(lower, "failed to sign"):
			return "jarsigner_sign_failed", "signing"
		case strings.Contains(lower, "self-signed"),
			strings.Contains(lower, "not timestamped"),
			strings.Contains(lower, "will be treated as unsigned"),
			strings.Contains(lower, "certificate chain not validated"):
			return "jarsigner_trust_warning", "signing"
		case strings.Contains(lower, "duplicate entry"):
			return "jarsigner_duplicate_entry", "signing"
		}
	case "zipalign":
		switch {
		case strings.Contains(lower, "can't open"),
			strings.Contains(lower, "cannot open"),
			strings.Contains(lower, "failed to open"),
			strings.Contains(lower, "failed to align"),
			strings.Contains(lower, "not a zip file"),
			strings.Contains(lower, "zipalign failed"):
			return "zipalign_failed", "packaging"
		case strings.Contains(lower, "not aligned"),
			strings.Contains(lower, "alignment warning"),
			strings.Contains(lower, "unaligned"):
			return "zipalign_unaligned_entry", "packaging"
		}
	case "lint":
		switch {
		case strings.Contains(lower, "missing permission") || strings.Contains(lower, "requires permission"):
			return "lint_missing_permission", "permissions"
		case strings.Contains(lower, "unused resource") || strings.Contains(lower, "the resource") && strings.Contains(lower, "appears to be unused"):
			return "lint_unused_resource", "unused-code"
		case strings.Contains(lower, "hardcoded text") || strings.Contains(lower, "hardcoded string"):
			return "lint_hardcoded_text", "i18n"
		case strings.Contains(lower, "missing translation") || strings.Contains(lower, "is not translated"):
			return "lint_missing_translation", "i18n"
		case strings.Contains(lower, "deprecated"):
			return "lint_deprecated_api", "deprecation"
		case strings.Contains(lower, "overdraw") || strings.Contains(lower, "useless parent"):
			return "lint_layout_performance", "performance"
		case strings.Contains(lower, "obsolete") || strings.Contains(lower, "gradle") && strings.Contains(lower, "obsolete"):
			return "lint_obsolete_dependency", "dependencies"
		case strings.Contains(lower, "missing constraints") || strings.Contains(lower, "not constrained"):
			return "lint_missing_constraints", "layout"
		case strings.Contains(lower, "contentdescription") || strings.Contains(lower, "content description"):
			return "lint_accessibility", "accessibility"
		case strings.Contains(lower, "newapi") || strings.Contains(lower, "requires api level"):
			return "lint_new_api", "api-compatibility"
		case strings.Contains(lower, "inlinedapi") || strings.Contains(lower, "inlined api"):
			return "lint_inlined_api", "api-compatibility"
		}
	case "manifest-merger":
		switch {
		case strings.Contains(lower, "conflict") && strings.Contains(lower, "attribute"):
			return "manifest_attribute_conflict", "manifest-merging"
		case strings.Contains(lower, "placeholder") && (strings.Contains(lower, "not found") || strings.Contains(lower, "unresolved")):
			return "manifest_missing_placeholder", "manifest-merging"
		case strings.Contains(lower, "uses-sdk") || strings.Contains(lower, "minsdkversion") || strings.Contains(lower, "targetsdkversion"):
			return "manifest_sdk_conflict", "manifest-merging"
		case strings.Contains(lower, "duplicate element") || strings.Contains(lower, "duplicate declaration"):
			return "manifest_duplicate_element", "manifest-merging"
		case strings.Contains(lower, "suggestion:") || strings.Contains(lower, "tools:replace") || strings.Contains(lower, "tools:node"):
			return "manifest_merge_suggestion", "manifest-merging"
		case strings.Contains(lower, "merge failed") || strings.Contains(lower, "merging failed"):
			return "manifest_merge_failed", "manifest-merging"
		}
	}
	return diagnosticCode(tool, severity), diagnosticCategory(tool)
}

func classifyLintIssueID(issueID string) string {
	switch issueID {
	case "hardcodedtext", "missingtranslation", "extratext", "typos", "settext_i18n":
		return "i18n"
	case "contentdescription", "labelfora11y", "imagecontrastcheck":
		return "accessibility"
	case "missingpermission", "unusedpermission":
		return "permissions"
	case "newapi", "inlinedapi", "obsoletesdkint":
		return "api-compatibility"
	case "unusedresources", "unusedids", "unusedattribute":
		return "unused-code"
	case "overdraw", "uselessparent", "toomanylayouts", "mergerootframe":
		return "performance"
	case "missingconstraints", "notsibling", "diffutils":
		return "layout"
	case "obsoletegradledependency", "gradledependency", "newerlibraryavailable":
		return "dependencies"
	case "deprecated", "deprecation":
		return "deprecation"
	case "sdkcardpath", "worldreadablefiles", "worldwriteablefiles", "unprotectedsmsmessage":
		return "security"
	default:
		return issueID
	}
}

func diagnosticCode(tool, severity string) string {
	if tool == "" {
		tool = "tool"
	}
	if severity == "" {
		severity = "message"
	}
	return tool + "_" + severity
}

func diagnosticCategory(tool string) string {
	if strings.TrimSpace(tool) == "" {
		return "tool"
	}
	return tool
}

func extractBracketCode(message string) (string, string, bool) {
	matches := bracketCode.FindStringSubmatch(strings.TrimSpace(message))
	if len(matches) != 3 {
		matches = failureBracketCode.FindStringSubmatch(strings.TrimSpace(message))
		if len(matches) < 2 {
			return "", message, false
		}
		rest := message
		if len(matches) >= 3 && strings.TrimSpace(matches[2]) != "" {
			rest = matches[2]
		}
		return matches[1], rest, true
	}
	return matches[1], matches[2], true
}

func isJavaRuntimeExceptionLine(message string) bool {
	return javaExceptionLine.MatchString(strings.TrimSpace(message))
}

func sanitizeDiagnosticToken(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, ".", "_")
	value = strings.ReplaceAll(value, "-", "_")
	return value
}

func extractRelatedDependency(message string) string {
	matches := dependencyCoordinate.FindStringSubmatch(message)
	if len(matches) != 2 {
		return ""
	}
	return matches[1]
}

func parseInt(value string) int {
	out, _ := strconv.Atoi(strings.TrimSpace(value))
	return out
}
