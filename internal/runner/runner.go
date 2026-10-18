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
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// runTag is the environment variable the runner sets on every child it
// starts. Its value is the run's tag — the sixteen random bytes the service
// minted for it, held by no other process, and the value the audit line
// records — and its presence in a live process's environment is where a boot
// reads that the process is this service's own run (proc(5) holds a process's
// environment at `/proc/[pid]/environ`).
const runTag = "DISPATCHER_RUN"

// procRoot is the process table a boot reads to find the runs that outlived
// the service it restarts.
const procRoot = "/proc"

// reapBound is how long a killed surviving group is given to leave the
// process table before a boot gives up on it and stops rather than serve a
// profile it could not free.
const reapBound = 5 * time.Second

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
// tag the audit line records, and the child's end.
type run struct {
	profile  string
	id       string
	tag      string
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
// wake for a profile that already has a run returns that run's id and tag and
// starts nothing. A run that cannot be started is an error, and fires no end.
func (r *Runner) Start(ctx context.Context, profile, reason string) (runID, tag string, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", fmt.Errorf("start run for %s: %w", profile, err)
	}
	r.mu.Lock()
	if live, ok := r.table[profile]; ok && !live.exited {
		r.mu.Unlock()
		return live.id, live.tag, nil
	}
	r.seq++
	id := fmt.Sprintf("%s-%d", profile, r.seq)
	tag, err = newTag()
	if err != nil {
		r.mu.Unlock()
		return "", "", fmt.Errorf("start run for %s: %w", profile, err)
	}
	cmd := exec.CommandContext(ctx, r.hermes, "--profile", profile, "chat", "-q", reason)
	// The child carries the run tag in its environment, so a boot after a
	// stop that left it alive recognises it as this service's own run and
	// ends it. The tag is the value the audit line records for the run.
	cmd.Env = append(os.Environ(), runTag+"="+tag)
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
		tag:     tag,
		cmd:     cmd,
	}
	r.table[profile] = tracked
	r.mu.Unlock()
	go r.reap(tracked)
	return id, tag, nil
}

// Running reports whether the profile has a run in flight. A run that ended —
// exited or killed — is not running, even before its replacement starts.
func (r *Runner) Running(profile string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	live, ok := r.table[profile]
	return ok && !live.exited
}

// Recover ends the runs that outlived the service and reports how many it
// reaped. A run is a child of the service: a stop that cancels the boot's
// context — SIGTERM, and so a clean stop — kills every run's group through
// exec.CommandContext and leaves nothing behind. A stop that does not —
// SIGKILL, and so a crash or a failure restart — leaves each live child
// reparented to init, leading its own process group and carrying the run
// tag, and the table that held it is gone. That child is a run no table
// holds: the boot look would start a second run for its profile without
// seeing the first, and one-run-per-agent, which reads the table, could not
// stop it. Recover is called before that first look; it kills each surviving
// group, as a bound or a context kill would, and waits for it to go, so the
// profile is free and the boot look's run is the only one.
func (r *Runner) Recover() (int, error) {
	survivors, err := r.survivors(procRoot)
	if err != nil {
		return 0, err
	}
	for _, pid := range survivors {
		killGroup(pid)
	}
	for _, pid := range survivors {
		if err := gone(procRoot, pid, reapBound); err != nil {
			return 0, err
		}
	}
	return len(survivors), nil
}

// survivors lists this service's own runs that outlived it: the live
// processes under proc that carry the run tag in their environment, name the
// Hermes binary this runner launches in their command line, and lead their
// own process group. The tag is what marks a process as the service's own
// run — its value is the tag the service minted — and the command line's
// `--profile` names the profile it runs; the binary name keeps the
// recognition to this service's children. A child that forked without
// exec'ing keeps its parent's command line and group, so the leader — the
// one whose group id is its own pid — is the single process a run answers
// as; killing the group it leads reaches every fork and grandchild under it.
// A read that fails is skipped, not fatal: a process may end between the
// walk and the read, and one the service may not read cannot be its child.
func (r *Runner) survivors(proc string) ([]int, error) {
	entries, err := os.ReadDir(proc)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", proc, err)
	}
	var leaders []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // not a pid directory
		}
		if !r.owns(proc, pid) {
			continue
		}
		if pgid, err := groupOf(proc, pid); err != nil || pgid != pid {
			continue // a fork under the run, which the leader's group covers
		}
		leaders = append(leaders, pid)
	}
	return leaders, nil
}

// owns reports whether a live process is one of this service's runs: its
// environment carries the run tag, and the command line it was started with
// names the Hermes binary this runner launches.
func (r *Runner) owns(proc string, pid int) bool {
	pidDir := filepath.Join(proc, strconv.Itoa(pid))
	environ, err := os.ReadFile(filepath.Join(pidDir, "environ"))
	if err != nil || !carriesTag(environ) {
		return false
	}
	cmdline, err := os.ReadFile(filepath.Join(pidDir, "cmdline"))
	if err != nil {
		return false
	}
	for _, arg := range bytes.Split(cmdline, []byte{0}) {
		if string(arg) == r.hermes {
			return true
		}
	}
	return false
}

// carriesTag reports whether an environment carries the run tag.
func carriesTag(environ []byte) bool {
	for _, entry := range bytes.Split(environ, []byte{0}) {
		if bytes.HasPrefix(entry, []byte(runTag+"=")) {
			return true
		}
	}
	return false
}

// groupOf is the process group a live process leads or belongs to, straight
// from its stat line: the leader of a group reads its own pid.
func groupOf(proc string, pid int) (int, error) {
	raw, err := os.ReadFile(filepath.Join(proc, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	stat := string(raw)
	fields := strings.Fields(stat[strings.LastIndexByte(stat, ')')+1:])
	if len(fields) < 3 {
		return 0, fmt.Errorf("stat of %d is short", pid)
	}
	return strconv.Atoi(fields[2]) // state, ppid, pgrp
}

// gone waits, up to bound, for a killed process to leave the process table,
// so the profile it ran is free before a run for it starts.
func gone(proc string, pid int, bound time.Duration) error {
	deadline := time.Now().Add(bound)
	for {
		if _, err := os.Stat(filepath.Join(proc, strconv.Itoa(pid))); err != nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("run %d is live %v after its kill", pid, bound)
		}
		time.Sleep(5 * time.Millisecond)
	}
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

// newTag mints the tag the audit line records for a run: the value the run's
// own process carries in its environment, sixteen random bytes as hex.
func newTag() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("mint tag: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
