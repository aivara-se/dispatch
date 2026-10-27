// Package run starts a Hermes profile's agent as a child process and forgets it.
package run

import (
	"errors"
	"fmt"
	"os/exec"
)

// Runner starts runs with the hermes binary at its configured absolute path and
// the environment it gives a child.
type Runner struct {
	path string
	env  []string
}

// New returns a Runner for the hermes binary at path, starting each child with
// exactly env, "NAME=value" pairs. That list is the whole of what a run
// inherits: the service's own environment is never passed on.
func New(path string, env []string) *Runner {
	return &Runner{path: path, env: env}
}

// Start launches `hermes --profile <profile> chat -q "<reason>" --source
// tool -c "<title>" --create-if-missing`, waits on the child from its own
// goroutine, and reports the exit status once. The title names the session the
// run resumes, created when it does not exist. An error means the child did not
// start.
func (r *Runner) Start(profile, reason, title string, onExit func(exit int)) error {
	cmd := exec.Command(r.path, "--profile", profile, "chat", "-q", reason, "--source", "tool", "-c", title, "--create-if-missing")
	cmd.Env = r.env
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
