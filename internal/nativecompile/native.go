package nativecompile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
)

type Compiler struct {
	tracker perf.Tracker
}

func New() *Compiler {
	return &Compiler{tracker: perf.New(false)}
}

func (c *Compiler) SetTracker(tracker perf.Tracker) {
	if tracker == nil {
		c.tracker = perf.New(false)
		return
	}
	c.tracker = tracker
}

func (c *Compiler) track(name string, fn func() error) error {
	if c.tracker == nil {
		return fn()
	}
	return c.tracker.Track(name, fn)
}

func (c *Compiler) beginSerial(name string) func() {
	if c.tracker == nil || !c.tracker.IsEnabled() {
		return func() {}
	}
	c.tracker = c.tracker.Serial(name)
	return func() {
		c.tracker = c.tracker.End()
	}
}

func (c *Compiler) CompileDebug(ctx context.Context, prj *project.Project, modulePath string, stdout, stderr *os.File) error {
	return c.CompileVariant(ctx, prj, modulePath, "debug", stdout, stderr)
}

func (c *Compiler) CompileVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error {
	mod := prj.FindModule(modulePath)
	if mod == nil {
		return fmt.Errorf("module %s not found", modulePath)
	}
	return c.compileMain(ctx, prj, mod, variantName, stdout, stderr)
}

func (c *Compiler) AssembleDebug(ctx context.Context, prj *project.Project, modulePath string, stdout, stderr *os.File) error {
	return c.AssembleVariant(ctx, prj, modulePath, "debug", stdout, stderr)
}

func (c *Compiler) AssembleVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error {
	mod := prj.FindModule(modulePath)
	if mod == nil {
		return fmt.Errorf("module %s not found", modulePath)
	}
	var mainOut string
	var runtimeCP []string
	var resources []androidResourceArtifact
	err := c.track("compileMain", func() error {
		var innerErr error
		mainOut, runtimeCP, resources, innerErr = c.compileMainInternal(ctx, prj, mod, variantName, newCompileState(), nil, stdout, stderr)
		return innerErr
	})
	if err != nil {
		return err
	}
	runtimeCP = excludePath(runtimeCP, filepath.Join(filepath.Dir(mainOut), "module-classes.jar"))
	variant := mod.Variant(variantName)
	var apkPath string
	err = c.track("assembleAPK", func() error {
		var innerErr error
		apkPath, innerErr = assembleAPK(ctx, prj, mod, variant, mainOut, runtimeCP, resources, stdout, stderr, c.tracker)
		return innerErr
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "assembled %s APK: %s\n", variantName, apkPath)
	return nil
}

func (c *Compiler) InstallVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, deviceSerial string, stdout, stderr *os.File) error {
	mod := prj.FindModule(modulePath)
	if mod == nil {
		return fmt.Errorf("module %s not found", modulePath)
	}
	var mainOut string
	var runtimeCP []string
	var resources []androidResourceArtifact
	err := c.track("compileMain", func() error {
		var innerErr error
		mainOut, runtimeCP, resources, innerErr = c.compileMainInternal(ctx, prj, mod, variantName, newCompileState(), nil, stdout, stderr)
		return innerErr
	})
	if err != nil {
		return err
	}
	runtimeCP = excludePath(runtimeCP, filepath.Join(filepath.Dir(mainOut), "module-classes.jar"))
	variant := mod.Variant(variantName)
	var apkPath string
	err = c.track("assembleAPK", func() error {
		var innerErr error
		apkPath, innerErr = assembleAPK(ctx, prj, mod, variant, mainOut, runtimeCP, resources, stdout, stderr, c.tracker)
		return innerErr
	})
	if err != nil {
		return err
	}
	if err := c.track("installAPK", func() error {
		return installAPK(ctx, apkPath, deviceSerial, stdout, stderr)
	}); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "installed %s APK: %s\n", variantName, apkPath)
	return nil
}

func (c *Compiler) compileMain(ctx context.Context, prj *project.Project, mod *project.Module, variantName string, stdout, stderr *os.File) error {
	_, _, _, err := c.compileMainInternal(ctx, prj, mod, variantName, newCompileState(), nil, stdout, stderr)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "native %s compilation complete\n", variantName)
	return nil
}

func (c *Compiler) CompileDebugAndroidTest(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error {
	mod := prj.FindModule(modulePath)
	if mod == nil {
		return fmt.Errorf("module %s not found", modulePath)
	}
	return c.compileAndroidTest(ctx, prj, mod, variantName, stdout, stderr)
}
