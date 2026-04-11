package nativecompile

import (
	"context"
	"testing"

	"github.com/kaeawc/grit/internal/tooldiag"
)

func TestParseToolDiagnosticsStructuredLocations(t *testing.T) {
	records := parseToolDiagnostics("kotlinc", "stderr", `
/repo/app/src/main/java/App.kt:7:3: warning: variable is never used
/repo/app/src/main/java/App.kt:9:11: error: unresolved reference: missingSymbol
`)
	if len(records) != 2 {
		t.Fatalf("expected 2 diagnostics, got %#v", records)
	}
	if records[0].Severity != "warning" || records[0].File != "/repo/app/src/main/java/App.kt" || records[0].Line != 7 || records[0].Column != 3 {
		t.Fatalf("unexpected first diagnostic: %#v", records[0])
	}
	if records[0].Code != "kotlinc_unused_symbol" || records[0].Category != "unused-code" || records[0].SourceKind != "tool-emitted" || records[0].Stream != "stderr" {
		t.Fatalf("unexpected classified first diagnostic: %#v", records[0])
	}
	if records[1].Severity != "error" || records[1].Code != "kotlinc_unresolved_reference" || records[1].Category != "symbol-resolution" || records[1].Line != 9 || records[1].Column != 11 {
		t.Fatalf("unexpected second diagnostic: %#v", records[1])
	}
}

func TestParseToolDiagnosticsFallsBackToSeverityInference(t *testing.T) {
	records := parseToolDiagnostics("aapt2", "stdout", `
error: failed linking references
warning: duplicate resource detected
TRACE aapt2 link args:
`)
	if len(records) != 2 {
		t.Fatalf("expected inferred diagnostics, got %#v", records)
	}
	if records[0].Severity != "error" || records[1].Severity != "warning" {
		t.Fatalf("unexpected inferred severities: %#v", records)
	}
	if records[0].Code != "aapt2_link_failed" || records[0].Category != "resource-linking" || records[0].Stream != "stdout" {
		t.Fatalf("unexpected first inferred diagnostic: %#v", records[0])
	}
	if records[1].Code != "aapt2_duplicate_resource" || records[1].Category != "resources" {
		t.Fatalf("unexpected second inferred diagnostic: %#v", records[1])
	}
}

func TestParseToolDiagnosticsExtractsRelatedDependency(t *testing.T) {
	records := parseToolDiagnostics("r8", "stderr", `
error: Missing class com.example.Foo referenced from com.squareup.okhttp3:okhttp:4.12.0
`)
	if len(records) != 1 {
		t.Fatalf("expected 1 diagnostic, got %#v", records)
	}
	if records[0].Code != "r8_missing_class" || records[0].Category != "shrinking" || records[0].RelatedDependency != "com.squareup.okhttp3:okhttp:4.12.0" {
		t.Fatalf("unexpected dependency-attributed diagnostic: %#v", records[0])
	}
}

func TestParseToolDiagnosticsParsesKotlinPrefixFormat(t *testing.T) {
	records := parseToolDiagnostics("kotlinc", "stderr", `
e: file:///repo/app/src/main/java/App.kt: (9, 11): unresolved reference: missingSymbol
w: /repo/app/src/main/java/App.kt: (7, 3): variable is never used
`)
	if len(records) != 2 {
		t.Fatalf("expected 2 diagnostics, got %#v", records)
	}
	if records[0].Code != "kotlinc_unresolved_reference" || records[0].File != "/repo/app/src/main/java/App.kt" {
		t.Fatalf("unexpected kotlin prefix diagnostic: %#v", records[0])
	}
	if records[1].Code != "kotlinc_unused_symbol" || records[1].Severity != "warning" {
		t.Fatalf("unexpected kotlin prefix warning: %#v", records[1])
	}
}

func TestParseToolDiagnosticsExtractsBracketCode(t *testing.T) {
	records := parseToolDiagnostics("javac", "stderr", `
/repo/app/src/main/java/App.java:12: warning: [deprecation] oldApi() in Legacy has been deprecated
`)
	if len(records) != 1 {
		t.Fatalf("expected 1 diagnostic, got %#v", records)
	}
	if records[0].Code != "javac_deprecation" || records[0].Category != "deprecation" {
		t.Fatalf("unexpected bracket-code diagnostic: %#v", records[0])
	}
}

func TestParseToolDiagnosticsClassifiesADBInstallAndUninstallFailures(t *testing.T) {
	records := parseToolDiagnostics("adb", "stderr", `
adb: failed to install /tmp/app.apk: Failure [INSTALL_FAILED_UPDATE_INCOMPATIBLE: Package signatures do not match]
Failure [DELETE_FAILED_INTERNAL_ERROR]
Unknown package: com.example.missing
`)
	if len(records) != 3 {
		t.Fatalf("expected 3 diagnostics, got %#v", records)
	}
	if records[0].Severity != "error" || records[0].Code != "adb_install_failed" || records[0].Category != "device-install" {
		t.Fatalf("unexpected adb install failure diagnostic: %#v", records[0])
	}
	if records[1].Severity != "error" || records[1].Code != "adb_uninstall_failed" || records[1].Category != "device-install" {
		t.Fatalf("unexpected adb delete failure diagnostic: %#v", records[1])
	}
	if records[2].Severity != "error" || records[2].Code != "adb_uninstall_failed" || records[2].Category != "device-install" {
		t.Fatalf("unexpected adb unknown-package diagnostic: %#v", records[2])
	}
}

func TestParseToolDiagnosticsClassifiesJavaRuntimeExceptionLines(t *testing.T) {
	records := parseToolDiagnostics("java", "stderr", `
Exception in thread "main" java.lang.IllegalStateException: boom
Caused by: java.lang.RuntimeException: wrapped
java.lang.AssertionError: broken
`)
	if len(records) != 3 {
		t.Fatalf("expected 3 diagnostics, got %#v", records)
	}
	if records[0].Severity != "error" || records[0].Code != "java_exception" || records[0].Category != "runtime" {
		t.Fatalf("unexpected java exception diagnostic: %#v", records[0])
	}
	if records[1].Severity != "error" || records[1].Code != "java_exception" || records[1].Category != "runtime" {
		t.Fatalf("unexpected caused-by diagnostic: %#v", records[1])
	}
	if records[2].Severity != "error" || records[2].Code != "java_error" || records[2].Category != "runtime" {
		t.Fatalf("unexpected java error diagnostic: %#v", records[2])
	}
}

func TestRecordToolDiagnosticsCapturesJavacSummaryWarningsAcrossStreams(t *testing.T) {
	collector := &tooldiag.Collector{}
	ctx := tooldiag.WithCollector(context.Background(), collector)

	recordToolDiagnostics(ctx, "javac", `
Note: /repo/app/src/main/java/App.java uses or overrides a deprecated API.
Note: Recompile with -Xlint:deprecation for details.
`, `
warning: [options] system modules path not set in conjunction with -source 21
`)

	records := collector.Records()
	if len(records) != 3 {
		t.Fatalf("expected 3 collected diagnostics, got %#v", records)
	}
	if records[0].Severity != "info" || records[0].Code != "javac_deprecated_api" || records[0].Category != "deprecation" || records[0].Stream != "stderr" {
		t.Fatalf("unexpected deprecated-api summary diagnostic: %#v", records[0])
	}
	if records[1].Severity != "info" || records[1].Code != "javac_deprecated_api" || records[1].Category != "deprecation" || records[1].Stream != "stderr" {
		t.Fatalf("unexpected xlint summary diagnostic: %#v", records[1])
	}
	if records[2].Severity != "warning" || records[2].Code != "javac_warning" || records[2].Category != "javac" || records[2].Stream != "stdout" {
		t.Fatalf("unexpected stdout warning diagnostic: %#v", records[2])
	}
}

func TestParseToolDiagnosticsClassifiesAPKSignerFailuresAndWarnings(t *testing.T) {
	records := parseToolDiagnostics("apksigner", "stderr", `
Failed to load signer "signer #1": java.io.IOException: Keystore was tampered with, or password was incorrect
WARNING: META-INF/services/example.Service not protected by this signature.
`)
	if len(records) != 2 {
		t.Fatalf("expected 2 diagnostics, got %#v", records)
	}
	if records[0].Severity != "error" || records[0].Code != "apksigner_sign_failed" || records[0].Category != "signing" {
		t.Fatalf("unexpected apksigner failure diagnostic: %#v", records[0])
	}
	if records[1].Severity != "warning" || records[1].Code != "apksigner_unprotected_entry" || records[1].Category != "signing" {
		t.Fatalf("unexpected apksigner warning diagnostic: %#v", records[1])
	}
}

func TestRecordToolDiagnosticsCapturesAPKSignerDiagnostics(t *testing.T) {
	collector := &tooldiag.Collector{}
	ctx := tooldiag.WithCollector(context.Background(), collector)

	recordToolDiagnostics(ctx, "apksigner", `
Failed to sign using signer "signer #1": private key mismatch
`, `
WARNING: signer #1 entry META-INF/MANIFEST.MF not protected by this signature.
`)

	records := collector.Records()
	if len(records) != 2 {
		t.Fatalf("expected 2 collected apksigner diagnostics, got %#v", records)
	}
	if records[0].Stream != "stderr" || records[0].Code != "apksigner_sign_failed" || records[0].Category != "signing" {
		t.Fatalf("unexpected collected apksigner failure diagnostic: %#v", records[0])
	}
	if records[1].Stream != "stdout" || records[1].Code != "apksigner_unprotected_entry" || records[1].Category != "signing" {
		t.Fatalf("unexpected collected apksigner warning diagnostic: %#v", records[1])
	}
}
