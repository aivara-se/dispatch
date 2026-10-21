// Package run starts a Hermes profile's agent as a child process and forgets it.
package run

import (
	"errors"
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

// Start launches `hermes --profile <profile> chat -q "<reason>" --source
// dispatch`, waits on the child from its own goroutine, and reports the exit
// status once. An error means the child did not start.
func (r *Runner) Start(profile, reason string, onExit func(exit int)) error {
	cmd := exec.Command(r.path, "--profile", profile, "chat", "-q", reason, "--source", "dispatch")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start hermes: %w", err)
	}
	go func() { onExit(exitCode(cmd.Wait())) }()
	return nil
}

// exitCode is a finished child's status: zero for success, its own code for a
// non-zero exit, and -1 when it did not exit on its own.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}
