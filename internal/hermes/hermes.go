// Package hermes launches one Hermes one-shot and reads the session it made.
package hermes

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
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

// sessionID reads the last session_id line Hermes writes to stderr.
func sessionID(stderr string) string {
	id := ""
	for _, line := range strings.Split(stderr, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "session_id:"); ok {
			id = strings.TrimSpace(value)
		}
	}
	return id
}

// stateDB is the profile's session store.
func (r *Runner) stateDB(profile string) string {
	if profile == "" || profile == "default" {
		return filepath.Join(r.home, "state.db")
	}
	return filepath.Join(r.home, "profiles", profile, "state.db")
}

// tools reads the session's tool calls. A missing session or store leaves
// them empty; the action's output stands on its own.
func (r *Runner) tools(profile, session string) (string, error) {
	if session == "" || r.home == "" {
		return "", nil
	}
	path := r.stateDB(profile)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return "", err
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT tool_calls FROM messages WHERE session_id = ? AND tool_calls IS NOT NULL ORDER BY id`, session)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var calls []json.RawMessage
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return "", err
		}
		var items []json.RawMessage
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			continue
		}
		calls = append(calls, items...)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(calls) == 0 {
		return "", nil
	}
	body, err := json.Marshal(calls)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
