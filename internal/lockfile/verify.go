package lockfile

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// MismatchKind categorizes one semantic difference between two lockfiles.
type MismatchKind string

const (
	MismatchKindPinCount       MismatchKind = "pin_count"
	MismatchKindMissingPin     MismatchKind = "missing_pin"
	MismatchKindUnexpectedPin  MismatchKind = "unexpected_pin"
	MismatchKindDuplicatePin   MismatchKind = "duplicate_pin"
	MismatchKindField          MismatchKind = "field_mismatch"
	MismatchKindFileCount      MismatchKind = "file_count"
	MismatchKindMissingFile    MismatchKind = "missing_file"
	MismatchKindUnexpectedFile MismatchKind = "unexpected_file"
	MismatchKindDuplicateFile  MismatchKind = "duplicate_file"
)

// Mismatch describes one semantic difference between two lockfiles.
type Mismatch struct {
	Kind         MismatchKind `json:"kind"`
	Coordinate   Coordinate   `json:"coordinate,omitempty"`
	RepositoryID string       `json:"repositoryId,omitempty"`
	FileKind     FileKind     `json:"fileKind,omitempty"`
	FileName     string       `json:"fileName,omitempty"`
	Field        string       `json:"field,omitempty"`
	Expected     string       `json:"expected,omitempty"`
	Actual       string       `json:"actual,omitempty"`
}

// VerifyResult describes whether two lockfiles are semantically equivalent.
// GeneratedAt and GritVersion are intentionally ignored so callers can use
// this as a CI gate over freshly produced resolver output without timestamp
// churn causing drift.
type VerifyResult struct {
	Match      bool       `json:"match"`
	Mismatches []Mismatch `json:"mismatches,omitempty"`
}

// Verify compares two lockfiles after canonicalization and returns a structured
// mismatch report. The comparison is semantic rather than byte-for-byte:
// ordering is normalized and GeneratedAt/GritVersion are ignored.
func Verify(expected, actual Lockfile) VerifyResult {
	expected = expected.Canonicalize()
	actual = actual.Canonicalize()

	var mismatches []Mismatch
	if len(expected.Pins) != len(actual.Pins) {
		mismatches = append(mismatches, Mismatch{
			Kind:     MismatchKindPinCount,
			Field:    "pins",
			Expected: strconv.Itoa(len(expected.Pins)),
			Actual:   strconv.Itoa(len(actual.Pins)),
		})
	}

	expectedByKey, expectedDupes := indexPins(expected.Pins)
	actualByKey, actualDupes := indexPins(actual.Pins)
	mismatches = append(mismatches, expectedDupes...)
	mismatches = append(mismatches, actualDupes...)

	expectedKeys := sortedKeys(expectedByKey)
	actualKeys := sortedKeys(actualByKey)

	for _, key := range expectedKeys {
		expectedPin := expectedByKey[key]
		actualPin, ok := actualByKey[key]
		if !ok {
			mismatches = append(mismatches, missingPinMismatch(expectedPin))
			continue
		}
		mismatches = append(mismatches, verifyPin(expectedPin, actualPin)...)
	}
	for _, key := range actualKeys {
		actualPin := actualByKey[key]
		if _, ok := expectedByKey[key]; !ok {
			mismatches = append(mismatches, unexpectedPinMismatch(actualPin))
		}
	}

	return VerifyResult{
		Match:      len(mismatches) == 0,
		Mismatches: mismatches,
	}
}

func indexPins(pins []Pin) (map[string]Pin, []Mismatch) {
	out := make(map[string]Pin, len(pins))
	var mismatches []Mismatch
	for _, pin := range pins {
		key := verifyPinKey(pin)
		if _, exists := out[key]; exists {
			mismatches = append(mismatches, Mismatch{
				Kind:         MismatchKindDuplicatePin,
				Coordinate:   pin.Coordinate,
				RepositoryID: pin.RepositoryID,
				Field:        "pins",
				Expected:     "unique pin key",
				Actual:       key,
			})
			continue
		}
		out[key] = pin
	}
	return out, mismatches
}

func sortedKeys[K ~string, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func verifyPinKey(pin Pin) string {
	return pin.Coordinate.String() + "|" + pin.RepositoryID
}

func verifyPin(expected, actual Pin) []Mismatch {
	var mismatches []Mismatch

	if !reflect.DeepEqual(expected.Attributes, actual.Attributes) {
		mismatches = append(mismatches, Mismatch{
			Kind:         MismatchKindField,
			Coordinate:   expected.Coordinate,
			RepositoryID: expected.RepositoryID,
			Field:        "attributes",
			Expected:     formatStringMap(expected.Attributes),
			Actual:       formatStringMap(actual.Attributes),
		})
	}
	if !reflect.DeepEqual(expected.Capabilities, actual.Capabilities) {
		mismatches = append(mismatches, Mismatch{
			Kind:         MismatchKindField,
			Coordinate:   expected.Coordinate,
			RepositoryID: expected.RepositoryID,
			Field:        "capabilities",
			Expected:     formatStrings(expected.Capabilities),
			Actual:       formatStrings(actual.Capabilities),
		})
	}
	if !reflect.DeepEqual(expected.Dependencies, actual.Dependencies) {
		mismatches = append(mismatches, Mismatch{
			Kind:         MismatchKindField,
			Coordinate:   expected.Coordinate,
			RepositoryID: expected.RepositoryID,
			Field:        "dependencies",
			Expected:     formatCoordinates(expected.Dependencies),
			Actual:       formatCoordinates(actual.Dependencies),
		})
	}
	if len(expected.Files) != len(actual.Files) {
		mismatches = append(mismatches, Mismatch{
			Kind:         MismatchKindFileCount,
			Coordinate:   expected.Coordinate,
			RepositoryID: expected.RepositoryID,
			Field:        "files",
			Expected:     strconv.Itoa(len(expected.Files)),
			Actual:       strconv.Itoa(len(actual.Files)),
		})
	}

	expectedFiles, expectedDupes := indexFiles(expected)
	actualFiles, actualDupes := indexFiles(actual)
	mismatches = append(mismatches, expectedDupes...)
	mismatches = append(mismatches, actualDupes...)

	expectedFileKeys := sortedKeys(expectedFiles)
	actualFileKeys := sortedKeys(actualFiles)

	for _, key := range expectedFileKeys {
		expectedFile := expectedFiles[key]
		actualFile, ok := actualFiles[key]
		if !ok {
			mismatches = append(mismatches, missingFileMismatch(expected, expectedFile))
			continue
		}
		if expectedFile.Hash != actualFile.Hash {
			mismatches = append(mismatches, fileFieldMismatch(expected, expectedFile, "hash", expectedFile.Hash.String(), actualFile.Hash.String()))
		}
		if expectedFile.Size != actualFile.Size {
			mismatches = append(mismatches, fileFieldMismatch(expected, expectedFile, "size", strconv.FormatInt(expectedFile.Size, 10), strconv.FormatInt(actualFile.Size, 10)))
		}
		if expectedFile.URL != actualFile.URL {
			mismatches = append(mismatches, fileFieldMismatch(expected, expectedFile, "url", expectedFile.URL, actualFile.URL))
		}
	}
	for _, key := range actualFileKeys {
		if _, ok := expectedFiles[key]; !ok {
			mismatches = append(mismatches, unexpectedFileMismatch(actual, actualFiles[key]))
		}
	}

	return mismatches
}

func verifyFileKey(file PinFile) string {
	return string(file.Kind) + "|" + file.Name
}

func indexFiles(pin Pin) (map[string]PinFile, []Mismatch) {
	out := make(map[string]PinFile, len(pin.Files))
	var mismatches []Mismatch
	for _, file := range pin.Files {
		key := verifyFileKey(file)
		if _, exists := out[key]; exists {
			mismatches = append(mismatches, Mismatch{
				Kind:         MismatchKindDuplicateFile,
				Coordinate:   pin.Coordinate,
				RepositoryID: pin.RepositoryID,
				FileKind:     file.Kind,
				FileName:     file.Name,
				Field:        "files",
				Expected:     "unique file key",
				Actual:       key,
			})
			continue
		}
		out[key] = file
	}
	return out, mismatches
}

func missingPinMismatch(pin Pin) Mismatch {
	return Mismatch{
		Kind:         MismatchKindMissingPin,
		Coordinate:   pin.Coordinate,
		RepositoryID: pin.RepositoryID,
		Expected:     "present",
		Actual:       "missing",
	}
}

func unexpectedPinMismatch(pin Pin) Mismatch {
	return Mismatch{
		Kind:         MismatchKindUnexpectedPin,
		Coordinate:   pin.Coordinate,
		RepositoryID: pin.RepositoryID,
		Expected:     "missing",
		Actual:       "present",
	}
}

func missingFileMismatch(pin Pin, file PinFile) Mismatch {
	return Mismatch{
		Kind:         MismatchKindMissingFile,
		Coordinate:   pin.Coordinate,
		RepositoryID: pin.RepositoryID,
		FileKind:     file.Kind,
		FileName:     file.Name,
		Expected:     "present",
		Actual:       "missing",
	}
}

func unexpectedFileMismatch(pin Pin, file PinFile) Mismatch {
	return Mismatch{
		Kind:         MismatchKindUnexpectedFile,
		Coordinate:   pin.Coordinate,
		RepositoryID: pin.RepositoryID,
		FileKind:     file.Kind,
		FileName:     file.Name,
		Expected:     "missing",
		Actual:       "present",
	}
}

func fileFieldMismatch(pin Pin, file PinFile, field, expected, actual string) Mismatch {
	return Mismatch{
		Kind:         MismatchKindField,
		Coordinate:   pin.Coordinate,
		RepositoryID: pin.RepositoryID,
		FileKind:     file.Kind,
		FileName:     file.Name,
		Field:        field,
		Expected:     expected,
		Actual:       actual,
	}
}

func formatStringMap(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, m[key]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func formatStrings(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	return "[" + strings.Join(values, ",") + "]"
}

func formatCoordinates(values []Coordinate) string {
	if len(values) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, value.String())
	}
	return "[" + strings.Join(parts, ",") + "]"
}
