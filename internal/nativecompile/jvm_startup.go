package nativecompile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type jvmStartupMode string

const (
	jvmStartupAuto   jvmStartupMode = "auto"
	jvmStartupAppCDS jvmStartupMode = "appcds"
	jvmStartupCRaC   jvmStartupMode = "crac"
	jvmStartupOff    jvmStartupMode = "off"
)

var (
	cracSupportOnce sync.Once
	cracSupported   bool
	cracSupportErr  error
)

func configuredJVMStartupMode() jvmStartupMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GRIT_JVM_STARTUP_MODE"))) {
	case "", "auto":
		return jvmStartupAuto
	case "appcds":
		return jvmStartupAppCDS
	case "crac":
		return jvmStartupCRaC
	case "off":
		return jvmStartupOff
	default:
		return jvmStartupAuto
	}
}

func runJavaMain(ctx context.Context, classpath []string, mainClass string, mainArgs []string, stdout, stderr *os.File) error {
	args, err := javaMainArgs(classpath, mainClass, mainArgs)
	if err != nil {
		return err
	}
	if err := prepareJavaStartupArgs(args); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "java", args...)
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(stderr, &stderrBuf)
	err = cmd.Run()
	recordToolDiagnostics(ctx, "java", stderrBuf.String(), stdoutBuf.String())
	return err
}

func javaMainArgs(classpath []string, mainClass string, mainArgs []string) ([]string, error) {
	args := javaStartupArgs(classpath, mainClass)
	if len(args) == 0 && configuredJVMStartupMode() == jvmStartupCRaC {
		supported, supportErr := supportsCRaC()
		if supportErr != nil {
			return nil, supportErr
		}
		if !supported {
			return nil, fmt.Errorf("CRaC requested but the current JVM/platform does not support it; use a CRaC-enabled JDK on Linux or use GRIT_JVM_STARTUP_MODE=appcds")
		}
		return nil, fmt.Errorf("CRaC requested but grit only supports AppCDS for one-shot JVM launches today; CRaC needs daemon/checkpoint lifecycle support")
	}
	args = append(args, "-cp", strings.Join(classpath, string(os.PathListSeparator)), mainClass)
	return append(args, mainArgs...), nil
}

func javaStartupArgs(classpath []string, mainClass string) []string {
	switch configuredJVMStartupMode() {
	case jvmStartupOff:
		return nil
	case jvmStartupCRaC:
		return nil
	default:
		if configuredJVMStartupMode() == jvmStartupAuto && classpathHasNonEmptyDir(classpath) {
			return nil
		}
		archive := appCDSArchivePath(classpath, mainClass)
		return []string{
			"-Xshare:auto",
			"-XX:+AutoCreateSharedArchive",
			"-XX:SharedArchiveFile=" + archive,
		}
	}
}

func classpathHasNonEmptyDir(classpath []string) bool {
	for _, entry := range classpath {
		info, err := os.Stat(entry)
		if err != nil || !info.IsDir() {
			continue
		}
		dirEntries, err := os.ReadDir(entry)
		if err == nil && len(dirEntries) > 0 {
			return true
		}
	}
	return false
}

func appCDSArchivePath(classpath []string, mainClass string) string {
	sum := sha256.New()
	sum.Write([]byte("appcds-v1"))
	sum.Write([]byte{0})
	sum.Write([]byte(mainClass))
	sum.Write([]byte{0})
	javaPath, _ := exec.LookPath("java")
	sum.Write([]byte(cacheIdentityForInput(javaPath)))
	sum.Write([]byte{0})
	for _, entry := range classpath {
		sum.Write([]byte(entry))
		sum.Write([]byte{0})
		sum.Write([]byte(appCDSInputIdentity(entry)))
		sum.Write([]byte{0})
	}
	return filepath.Join(sharedNativeCacheRoot(), "appcds", hex.EncodeToString(sum.Sum(nil))+".jsa")
}

func sharedArchiveFileArg(args []string) (string, bool) {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-XX:SharedArchiveFile=") {
			return strings.TrimPrefix(arg, "-XX:SharedArchiveFile="), true
		}
	}
	return "", false
}

func prepareJavaStartupArgs(args []string) error {
	if archive, ok := sharedArchiveFileArg(args); ok {
		if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
			return fmt.Errorf("prepare AppCDS cache dir: %w", err)
		}
		// Probabilistically evict stale archives (~1 in 20 launches).
		if rand.Intn(20) == 0 { // #nosec
			go evictStaleAppCDSArchives(filepath.Dir(archive), 7*24*time.Hour)
		}
	}
	return nil
}

// evictStaleAppCDSArchives removes .jsa files in dir that have not been
// modified within maxAge. This keeps the AppCDS cache from growing unboundedly
// as classpaths evolve over time.
func evictStaleAppCDSArchives(dir string, maxAge time.Duration) (removed int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsa") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if os.Remove(filepath.Join(dir, entry.Name())) == nil {
				removed++
			}
		}
	}
	return removed
}

func appCDSInputIdentity(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "missing"
	}
	if info.IsDir() {
		return "dir:" + dirFingerprint(path)
	}
	return cacheIdentityForInput(path)
}

func supportsCRaC() (bool, error) {
	cracSupportOnce.Do(func() {
		if runtime.GOOS != "linux" {
			cracSupported = false
			return
		}
		cmd := exec.Command("java", "--list-modules")
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			cracSupportErr = fmt.Errorf("check CRaC support: %w: %s", err, strings.TrimSpace(out.String()))
			return
		}
		cracSupported = strings.Contains(out.String(), "jdk.crac")
	})
	return cracSupported, cracSupportErr
}
