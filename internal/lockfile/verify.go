package lockfile

import (
	"fmt"
	"reflect"
)

// VerifyResult describes whether two lockfiles are semantically equivalent.
// GeneratedAt and GritVersion are intentionally ignored so callers can use
// this as a CI gate over resolver output without timestamp churn causing drift.
type VerifyResult struct {
	Match  bool     `json:"match"`
	Issues []string `json:"issues,omitempty"`
}

// Verify compares two lockfiles after canonicalization and returns a structured
// mismatch report. The comparison is semantic rather than byte-for-byte:
// ordering is normalized and GeneratedAt/GritVersion are ignored.
func Verify(expected, actual Lockfile) VerifyResult {
	expected = expected.Canonicalize()
	actual = actual.Canonicalize()

	var issues []string
	if len(expected.Pins) != len(actual.Pins) {
		issues = append(issues, fmt.Sprintf("pin count mismatch: expected %d got %d", len(expected.Pins), len(actual.Pins)))
	}

	expectedByKey := make(map[string]Pin, len(expected.Pins))
	for _, pin := range expected.Pins {
		expectedByKey[verifyPinKey(pin)] = pin
	}
	actualByKey := make(map[string]Pin, len(actual.Pins))
	for _, pin := range actual.Pins {
		actualByKey[verifyPinKey(pin)] = pin
	}

	for key, expectedPin := range expectedByKey {
		actualPin, ok := actualByKey[key]
		if !ok {
			issues = append(issues, fmt.Sprintf("missing pin: %s", key))
			continue
		}
		issues = append(issues, verifyPin(expectedPin, actualPin)...)
	}
	for key := range actualByKey {
		if _, ok := expectedByKey[key]; !ok {
			issues = append(issues, fmt.Sprintf("unexpected pin: %s", key))
		}
	}

	return VerifyResult{
		Match:  len(issues) == 0,
		Issues: issues,
	}
}

func verifyPinKey(pin Pin) string {
	return pin.Coordinate.String() + "|" + pin.RepositoryID
}

func verifyPin(expected, actual Pin) []string {
	var issues []string
	prefix := verifyPinKey(expected)

	if !reflect.DeepEqual(expected.Attributes, actual.Attributes) {
		issues = append(issues, fmt.Sprintf("attributes mismatch for %s", prefix))
	}
	if !reflect.DeepEqual(expected.Capabilities, actual.Capabilities) {
		issues = append(issues, fmt.Sprintf("capabilities mismatch for %s: expected %v got %v", prefix, expected.Capabilities, actual.Capabilities))
	}
	if !reflect.DeepEqual(expected.Dependencies, actual.Dependencies) {
		issues = append(issues, fmt.Sprintf("dependencies mismatch for %s: expected %v got %v", prefix, expected.Dependencies, actual.Dependencies))
	}
	if len(expected.Files) != len(actual.Files) {
		issues = append(issues, fmt.Sprintf("file count mismatch for %s: expected %d got %d", prefix, len(expected.Files), len(actual.Files)))
	}

	expectedFiles := make(map[string]PinFile, len(expected.Files))
	for _, file := range expected.Files {
		expectedFiles[verifyFileKey(file)] = file
	}
	actualFiles := make(map[string]PinFile, len(actual.Files))
	for _, file := range actual.Files {
		actualFiles[verifyFileKey(file)] = file
	}

	for key, expectedFile := range expectedFiles {
		actualFile, ok := actualFiles[key]
		if !ok {
			issues = append(issues, fmt.Sprintf("missing file for %s: %s", prefix, key))
			continue
		}
		if expectedFile.Hash != actualFile.Hash {
			issues = append(issues, fmt.Sprintf("hash mismatch for %s file %s: expected %s got %s", prefix, key, expectedFile.Hash, actualFile.Hash))
		}
		if expectedFile.Size != actualFile.Size {
			issues = append(issues, fmt.Sprintf("size mismatch for %s file %s: expected %d got %d", prefix, key, expectedFile.Size, actualFile.Size))
		}
		if expectedFile.URL != actualFile.URL {
			issues = append(issues, fmt.Sprintf("url mismatch for %s file %s: expected %q got %q", prefix, key, expectedFile.URL, actualFile.URL))
		}
	}
	for key := range actualFiles {
		if _, ok := expectedFiles[key]; !ok {
			issues = append(issues, fmt.Sprintf("unexpected file for %s: %s", prefix, key))
		}
	}

	return issues
}

func verifyFileKey(file PinFile) string {
	return string(file.Kind) + "|" + file.Name
}
