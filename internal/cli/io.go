package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type logCapture struct {
	stdoutPath string
	stderrPath string
	Stdout     *os.File
	Stderr     *os.File
}

func newLogCapture() (*logCapture, error) {
	stdoutFile, err := os.CreateTemp("", "grit-stdout-*.log")
	if err != nil {
		return nil, err
	}
	stderrFile, err := os.CreateTemp("", "grit-stderr-*.log")
	if err != nil {
		stdoutFile.Close()
		os.Remove(stdoutFile.Name())
		return nil, err
	}
	return &logCapture{
		stdoutPath: stdoutFile.Name(),
		stderrPath: stderrFile.Name(),
		Stdout:     stdoutFile,
		Stderr:     stderrFile,
	}, nil
}

func (c *logCapture) Logs() responseLogs {
	c.Stdout.Sync()
	c.Stderr.Sync()
	stdoutData, _ := os.ReadFile(c.stdoutPath)
	stderrData, _ := os.ReadFile(c.stderrPath)
	return responseLogs{
		Stdout: strings.TrimSpace(string(stdoutData)),
		Stderr: strings.TrimSpace(string(stderrData)),
	}
}

func (c *logCapture) Close() {
	if c.Stdout != nil {
		c.Stdout.Close()
		os.Remove(c.stdoutPath)
	}
	if c.Stderr != nil {
		c.Stderr.Close()
		os.Remove(c.stderrPath)
	}
}

func writeResponse(stdout io.Writer, resp response, exitCode int, stderr io.Writer) int {
	if resp.Logs != nil && resp.Logs.Stdout == "" && resp.Logs.Stderr == "" {
		resp.Logs = nil
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(resp); err != nil {
		fmt.Fprintf(stderr, "{\"success\":false,\"command\":\"internal-error\",\"error\":{\"message\":%q}}\n", err.Error())
		return 1
	}
	return exitCode
}

func resultJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{"error":"failed to encode result"}`)
	}
	return json.RawMessage(data)
}
