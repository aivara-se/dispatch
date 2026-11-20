// Package hermes launches one Hermes one-shot and reads the session it made.
package hermes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Runner launches the configured Hermes binary.
type Runner struct {
	binary string
	home   string
}

// Result is one prompt action's outcome.
type Result struct {
	Output  string
	Exit    int
	Session string
	Tools   string
}

// New returns a runner for the binary. The session store lives under
// HERMES_HOME, or ~/.hermes when it is unset.
func New(binary string) *Runner {
	home := os.Getenv("HERMES_HOME")
	if home == "" {
		if user, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(user, ".hermes")
		}
	}
	return &Runner{binary: binary, home: home}
}

// Run launches one one-shot for a profile. A non-zero exit is a result, not
// an error; a cancelled context is returned as ctx.Err().
func (r *Runner) Run(ctx context.Context, profile, text, key string) (Result, error) {
	args := []string{
		"--profile", profile,
		"chat", "-q", text, "-Q",
		"--source", "tool",
		"-c", key,
		"--create-if-missing",
	}
	command := exec.CommandContext(ctx, r.binary, args...)
	command.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()

	result := Result{Output: stdout.String()}
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, fmt.Errorf("hermes: %w", err)
		}
		result.Exit = exit.ExitCode()
	}
	result.Session = sessionID(stderr.String())
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	tools, err := r.tools(profile, result.Session)
	if err != nil {
		return result, err
	}
	result.Tools = tools
	return result, nil
}
