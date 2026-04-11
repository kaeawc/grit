package testsupport

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/tooldiag"
)

type CompilerRecorder struct {
	Calls       []string
	Err         error
	Diagnostics []tooldiag.Record
	tracker     perf.Tracker
	mu          sync.Mutex
}

func (f *CompilerRecorder) SetTracker(tracker perf.Tracker) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tracker = tracker
}

func (f *CompilerRecorder) CallsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.Calls...)
}

func (f *CompilerRecorder) CompileVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error {
	return f.record(ctx, "compile:"+modulePath+":"+variantName)
}

func (f *CompilerRecorder) AssembleVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error {
	return f.record(ctx, "assemble:"+modulePath+":"+variantName)
}

func (f *CompilerRecorder) InstallVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, deviceSerial string, stdout, stderr *os.File) error {
	return f.record(ctx, "install:"+modulePath+":"+variantName+":"+deviceSerial)
}

func (f *CompilerRecorder) TestDebugUnit(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error {
	return f.record(ctx, "test:"+modulePath+":"+variantName)
}

func (f *CompilerRecorder) CompileDebugUnit(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error {
	return f.record(ctx, "compile-tests:"+modulePath+":"+variantName)
}

func (f *CompilerRecorder) CompileDebugAndroidTest(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error {
	return f.record(ctx, "compile-android-tests:"+modulePath+":"+variantName)
}

func (f *CompilerRecorder) record(ctx context.Context, call string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, call)
	tooldiag.RecordAll(ctx, append([]tooldiag.Record(nil), f.Diagnostics...))
	if f.tracker != nil && f.tracker.IsEnabled() {
		switch {
		case strings.HasPrefix(call, "compile:"):
			_ = f.tracker.Track("compileKotlin", func() error { return nil })
		case strings.HasPrefix(call, "assemble:"):
			_ = f.tracker.Track("runD8", func() error { return nil })
		case strings.HasPrefix(call, "test:"):
			_ = f.tracker.Track("runJUnit", func() error { return nil })
		case strings.HasPrefix(call, "compile-tests:"):
			_ = f.tracker.Track("compileTests", func() error { return nil })
		case strings.HasPrefix(call, "compile-android-tests:"):
			_ = f.tracker.Track("compileAndroidTests", func() error { return nil })
		}
	}
	return f.Err
}
