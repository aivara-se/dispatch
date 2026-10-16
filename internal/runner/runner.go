// Package runner starts and tracks one bounded run per agent: the Hermes
// child the wake concerns, with the reason as its only input.
//
// A wake names a profile and carries the reason; the runner launches
// hermes --profile <profile> chat -q <reason> as a child of the service and
// tracks it until it ends. A wake for a profile that already has a run
// collapses onto that run — nothing queues, and no second child starts. A
// run past its bound is killed. Every run's end reaches onEnd exactly once,
// which is what wakes the next look for that agent.
package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Runner starts Hermes children and tracks one run per profile. It is built
// once, at boot, with the Hermes binary, the bound every run dies past, and
// the function a run's end reaches. The zero value is not usable.
type Runner struct {
	hermes string
	bound  time.Duration
	onEnd  func(string)

	mu    sync.Mutex
	seq   int
	table map[string]*run
}

// run is one tracked child: the profile it runs as, the local run id, the
// session id the audit line records, and the child's end.
type run struct {
	profile  string
	id       string
	session  string
	cmd      *exec.Cmd
	exited   bool
	exitCode int
}

// New builds a Runner. hermes is the Hermes binary a run launches, bound how
// long a run may live before it is killed (zero or negative disables the
// bound), and onEnd the function every run's end reaches exactly once, with
// the profile whose run ended.
func New(hermes string, bound time.Duration, onEnd func(string)) *Runner {
	return &Runner{
		hermes: hermes,
		bound:  bound,
		onEnd:  onEnd,
		table:  make(map[string]*run),
	}
}

// Start starts the profile's run with the reason, and records it. The reason
// is the run's only input, passed as the child's single question argument; it
// holds identifiers and the service's own words, never third-party text. A
// wake for a profile that already has a run returns that run's ids and starts
// nothing. A run that cannot be started is an error, and fires no end.
func (r *Runner) Start(ctx context.Context, profile, reason string) (runID, session string, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", fmt.Errorf("start run for %s: %w", profile, err)
	}
	r.mu.Lock()
	if live, ok := r.table[profile]; ok && !live.exited {
		id, sess := live.id, live.session
		r.mu.Unlock()
		return id, sess, nil
	}
	r.seq++
	id := fmt.Sprintf("%s-%d", profile, r.seq)
	sess, err := newSession()
	if err != nil {
		r.mu.Unlock()
		return "", "", fmt.Errorf("start run for %s: %w", profile, err)
	}
	session = sess
	cmd := exec.CommandContext(ctx, r.hermes, "--profile", profile, "chat", "-q", reason)
	// The child leads its own process group, so a kill reaches the group:
	// hermes can hatch children of its own, and a run past its bound or
	// past its context dies with all of them, not just the leader.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	if err = cmd.Start(); err != nil {
		r.mu.Unlock()
		return "", "", fmt.Errorf("start run for %s: %w", profile, err)
	}
	tracked := &run{
		profile: profile,
		id:      id,
		session: session,
		cmd:     cmd,
	}
	r.table[profile] = tracked
	r.mu.Unlock()
	go r.reap(tracked)
	return id, session, nil
}

// Running reports whether the profile has a run in flight. A run that ended —
// exited or killed — is not running, even before its replacement starts.
func (r *Runner) Running(profile string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	live, ok := r.table[profile]
	return ok && !live.exited
}

// reap waits for the child's end, records its exit code, and fires onEnd
// exactly once. It is the only caller of onEnd for its run, which is what
// makes exactly-once structural rather than raced.
func (r *Runner) reap(tracked *run) {
	var timer *time.Timer
	if r.bound > 0 {
		timer = time.AfterFunc(r.bound, func() {
			killGroup(tracked.cmd.Process.Pid)
		})
	}
	waitErr := tracked.cmd.Wait()
	if timer != nil {
		timer.Stop()
	}
	r.mu.Lock()
	tracked.exited = true
	tracked.exitCode = exitCode(waitErr)
	onEnd := r.onEnd
	r.mu.Unlock()
	onEnd(tracked.profile)
}

// killGroup kills the child's whole process group and reports the kill. A
// group that is already gone kills nothing, which is not an error.
func killGroup(pgid int) error {
	err := syscall.Kill(-pgid, syscall.SIGKILL)
	if err == syscall.ESRCH {
		return nil
	}
	return err
}

// exitCode is the child's exit code: zero for a clean exit, the process's
// code otherwise. A child killed by a signal has no code to keep, and keeps
// -1, which still reads as died rather than exited.
func exitCode(waitErr error) int {
	if waitErr == nil {
		return 0
	}
	if exited, ok := waitErr.(*exec.ExitError); ok {
		return exited.ExitCode()
	}
	return -1
}

// newSession mints the session id the audit line records for a run. It is the
// service's own handle for the run, sixteen random bytes as hex.
func newSession() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("mint session: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
