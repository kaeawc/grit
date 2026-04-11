package cli

import (
	"encoding/json"
	"io"
	"time"

	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/service"
)

type commandState struct {
	name    string
	stdout  io.Writer
	stderr  io.Writer
	tracker perf.Tracker
	start   time.Time
	svc     *service.Service
}

func newCommandState(name string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) commandState {
	return commandState{
		name:    name,
		stdout:  stdout,
		stderr:  stderr,
		tracker: tracker,
		start:   start,
		svc:     service.New(),
	}
}

func (c commandState) loadProject(repo string) (*project.Project, error) {
	var prj *project.Project
	err := c.tracker.Track("loadProject", func() error {
		var loadErr error
		prj, loadErr = c.svc.LoadProject(repo)
		return loadErr
	})
	if err != nil {
		return nil, err
	}
	return prj, nil
}

func (c commandState) requireModule(prj *project.Project, path string) (*project.Module, error) {
	return c.svc.RequireModule(prj, path)
}

func (c commandState) success(result json.RawMessage) int {
	return writeResponse(c.stdout, response{
		Success:    true,
		Command:    c.name,
		DurationMs: time.Since(c.start).Milliseconds(),
		Result:     result,
		PerfTiming: c.tracker.GetTimings(),
	}, 0, c.stderr)
}

func (c commandState) fail(code int, err error) int {
	return c.failWithResult(code, err, nil)
}

func (c commandState) failWithResult(code int, err error, result json.RawMessage) int {
	return writeResponse(c.stdout, response{
		Success:    false,
		Command:    c.name,
		DurationMs: time.Since(c.start).Milliseconds(),
		Result:     result,
		Error:      &responseError{Message: err.Error()},
		PerfTiming: c.tracker.GetTimings(),
	}, code, c.stderr)
}
