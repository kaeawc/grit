package nativecompile

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFindJstackHonorsJAVAHOME(t *testing.T) {
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(binDir, "jstack")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Strip jstack from PATH for this test so the JAVA_HOME fallback
	// is the only candidate. On systems where jstack is in PATH we
	// just verify findJstack returns *something* non-empty when a
	// JDK is installed; that's covered by TestFindJstackUsesPATH.
	t.Setenv("PATH", binDir)
	t.Setenv("JAVA_HOME", tmp)
	if got := findJstack(); got != fake {
		t.Fatalf("JAVA_HOME fallback: got %q want %q", got, fake)
	}
}

func TestFindJstackUsesPATH(t *testing.T) {
	if _, err := exec.LookPath("jstack"); err != nil {
		t.Skip("jstack not on PATH; nothing to verify")
	}
	got := findJstack()
	if got == "" {
		t.Fatal("expected non-empty jstack path when jstack is on PATH")
	}
	if !strings.HasSuffix(got, "jstack") && !strings.HasSuffix(got, "jstack.exe") {
		t.Fatalf("path does not look like jstack: %q", got)
	}
}

func TestFindJstackReturnsEmptyWhenAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("JAVA_HOME", t.TempDir())
	if got := findJstack(); got != "" {
		t.Fatalf("expected empty when jstack absent, got %q", got)
	}
}

func TestCaptureJVMThreadDumpMissingJstackReturnsEmpty(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("JAVA_HOME", t.TempDir())
	if got := captureJVMThreadDump(99999); got != "" {
		t.Fatalf("expected empty path when jstack unavailable, got %q", got)
	}
}

func TestCaptureJVMThreadDumpWritesFileAgainstFakeJstack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based fake jstack not portable to windows")
	}
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(binDir, "jstack")
	script := "#!/bin/sh\necho fake jstack pid=$2\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("JAVA_HOME", "")

	path := captureJVMThreadDump(4242)
	if path == "" {
		t.Fatal("expected non-empty dump path")
	}
	defer func() { _ = os.Remove(path) }()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "fake jstack pid=4242") {
		t.Fatalf("dump file missing expected content, got %q", contents)
	}
}
