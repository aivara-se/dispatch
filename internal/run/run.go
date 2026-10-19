// Package run starts a Hermes profile's agent as a child process and forgets it.
package run

import (
	"fmt"
	"os/exec"
)

// Runner starts runs with the hermes binary at its configured absolute path.
type Runner struct {
	path string
}

// New returns a Runner for the hermes binary at path.
func New(path string) *Runner {
	return &Runner{path: path}
}

// Start launches `hermes --profile <profile> chat -q "<reason>"` and reaps it.
// It keeps no record of the run: an error means the child did not start.
func (r *Runner) Start(profile, reason string) error {
	cmd := exec.Command(r.path, "--profile", profile, "chat", "-q", reason)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start hermes: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
