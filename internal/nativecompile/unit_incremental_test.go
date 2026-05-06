package nativecompile

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/testutil"
)

func TestSingleChangedSourceForIncrementalCompile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	outDir := filepath.Join(root, "classes")
	oldSource := testutil.WriteFile(t, srcDir, "OldTest.kt", "class OldTest")
	changedSource := testutil.WriteFile(t, srcDir, "ChangedTest.kt", "class ChangedTest")
	classFile := testutil.WriteFile(t, outDir, "ChangedTest.class", "bytecode")

	outputTime := time.Unix(1_700_000_000, 0)
	oldTime := outputTime.Add(-time.Minute)
	newTime := outputTime.Add(time.Minute)
	for path, ts := range map[string]time.Time{
		oldSource:     oldTime,
		changedSource: newTime,
		classFile:     outputTime,
		outDir:        outputTime,
	} {
		if err := os.Chtimes(path, ts, ts); err != nil {
			t.Fatalf("Chtimes %s: %v", path, err)
		}
	}

	got, ok := singleChangedSourceForIncrementalCompile([]string{oldSource, changedSource}, outDir)
	if !ok || got != changedSource {
		t.Fatalf("expected one changed source %q, got %q ok=%v", changedSource, got, ok)
	}
}

func TestSingleChangedSourceForIncrementalCompileRejectsMultipleChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	outDir := filepath.Join(root, "classes")
	first := testutil.WriteFile(t, srcDir, "FirstTest.kt", "class FirstTest")
	second := testutil.WriteFile(t, srcDir, "SecondTest.kt", "class SecondTest")
	classFile := testutil.WriteFile(t, outDir, "FirstTest.class", "bytecode")

	outputTime := time.Unix(1_700_000_000, 0)
	newTime := outputTime.Add(time.Minute)
	for path, ts := range map[string]time.Time{
		first:     newTime,
		second:    newTime,
		classFile: outputTime,
		outDir:    outputTime,
	} {
		if err := os.Chtimes(path, ts, ts); err != nil {
			t.Fatalf("Chtimes %s: %v", path, err)
		}
	}

	if got, ok := singleChangedSourceForIncrementalCompile([]string{first, second}, outDir); ok || got != "" {
		t.Fatalf("expected multiple changes to disable incremental compile, got %q ok=%v", got, ok)
	}
}

func TestSemanticSourceFingerprintIgnoresCommentsAndWhitespace(t *testing.T) {
	t.Parallel()

	first := []byte(`
package example

// comment
class Example(val value: String)
`)
	second := []byte(`
package example
class Example(
  val value: String /* comment */
)
`)
	if semanticSourceFingerprint(first) != semanticSourceFingerprint(second) {
		t.Fatal("expected comments and whitespace not to affect semantic source fingerprint")
	}
	changed := []byte(`package example class Example(val value: Int)`)
	if semanticSourceFingerprint(first) == semanticSourceFingerprint(changed) {
		t.Fatal("expected semantic source change to affect fingerprint")
	}
}
