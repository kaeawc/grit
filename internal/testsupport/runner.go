package testsupport

import (
	"sync"
)

type CommandInvocation struct {
	Name string
	Args []string
}

type CommandResponse struct {
	Stdout string
	Stderr string
	Err    error
}

type CommandRunner struct {
	mu        sync.Mutex
	responses map[string]CommandResponse
	calls     []CommandInvocation
}

func NewCommandRunner() *CommandRunner {
	return &CommandRunner{
		responses: map[string]CommandResponse{},
	}
}

func (r *CommandRunner) When(name string, args []string, response CommandResponse) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.responses[invocationKey(name, args)] = response
}

func (r *CommandRunner) Run(name string, args ...string) CommandResponse {
	if r == nil {
		return CommandResponse{Err: nil}
	}
	call := CommandInvocation{
		Name: name,
		Args: append([]string(nil), args...),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
	if response, ok := r.responses[invocationKey(name, args)]; ok {
		return response
	}
	return CommandResponse{}
}

func (r *CommandRunner) CallsSnapshot() []CommandInvocation {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]CommandInvocation(nil), r.calls...)
}

func (r *CommandRunner) Count(name string) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, call := range r.calls {
		if call.Name == name {
			count++
		}
	}
	return count
}

func invocationKey(name string, args []string) string {
	key := name
	for _, arg := range args {
		key += "\x00" + arg
	}
	return key
}
