// Package machinetest provides a fake machine.Runner for tests.
package machinetest

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/harlew-dev/lewbox/internal/machine"
)

// Runner records every command and answers from RunFn and OutputFn.
type Runner struct {
	RunFn    func(ctx context.Context, cmd machine.Cmd) error
	OutputFn func(ctx context.Context, cmd machine.Cmd) ([]byte, error)

	mu   sync.Mutex
	cmds []machine.Cmd
}

var _ machine.Runner = (*Runner)(nil)

// New returns an empty fake.
func New() *Runner { return &Runner{} }

// Run records cmd and calls RunFn.
func (r *Runner) Run(ctx context.Context, cmd machine.Cmd) error {
	r.record(cmd)
	if r.RunFn == nil {
		return nil
	}
	return r.RunFn(ctx, cmd)
}

// Output records cmd and calls OutputFn.
func (r *Runner) Output(ctx context.Context, cmd machine.Cmd) ([]byte, error) {
	r.record(cmd)
	if r.OutputFn == nil {
		return nil, fmt.Errorf("unexpected command: %s", cmd.Display())
	}
	return r.OutputFn(ctx, cmd)
}

func (r *Runner) record(cmd machine.Cmd) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cmds = append(r.cmds, cmd)
}

// Commands returns the recorded commands in order.
func (r *Runner) Commands() []machine.Cmd {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]machine.Cmd(nil), r.cmds...)
}

// Displays returns the recorded command lines in order.
func (r *Runner) Displays() []string {
	out := make([]string, 0)
	for _, cmd := range r.Commands() {
		out = append(out, cmd.Display())
	}
	return out
}

// Reset forgets the recorded commands.
func (r *Runner) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cmds = nil
}

// Ran reports whether a recorded command line contains every one of the
// given parts.
func (r *Runner) Ran(parts ...string) bool {
	for _, line := range r.Displays() {
		found := true
		for _, part := range parts {
			if !strings.Contains(line, part) {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}
	return false
}
