package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubHermes writes an executable shell script standing in for the Hermes
// binary. The script appends its arguments, one invocation per line, to
// argsLog, then behaves as directed by files in its directory: an `exitcode`
// file sets the exit status, and a `hold` file, while present, keeps it
// sleeping instead of exiting.
func stubHermes(t *testing.T) (binary, argsLog, dir string) {
	t.Helper()
	dir = t.TempDir()
	argsLog = filepath.Join(dir, "args.log")
	script := filepath.Join(dir, "hermes")
	body := `#!/bin/sh
echo "$@" >> "` + argsLog + `"
code=$(cat "` + dir + `/exitcode" 2>/dev/null || echo 0)
while [ -f "` + dir + `/hold" ]; do sleep 0.05; done
sleep "${STUB_SLEEP:-0}"
exit "$code"
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, argsLog, dir
}

func setExitCode(t *testing.T, dir string, code int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "exitcode"), []byte(strconv.Itoa(code)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func invocations(t *testing.T, argsLog string) []string {
	t.Helper()
	raw, err := os.ReadFile(argsLog)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

func waitEnd(t *testing.T, ended <-chan string, profile string) {
	t.Helper()
	select {
	case got := <-ended:
		if got != profile {
			t.Fatalf("onEnd fired for %q, want %q", got, profile)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("onEnd never fired for %q", profile)
	}
}

func TestReasonIsTheOnlyInput(t *testing.T) {
	hermes, argsLog, _ := stubHermes(t)
	ended := make(chan string, 4)
	r := New(hermes, time.Minute, func(profile string) { ended <- profile })
	if _, _, err := r.Start(context.Background(), "mimi", "card 73 is unsettled in Refined"); err != nil {
		t.Fatal(err)
	}
	waitEnd(t, ended, "mimi")
	lines := invocations(t, argsLog)
	if len(lines) != 1 {
		t.Fatalf("hermes invoked %d times, want 1: %q", len(lines), lines)
	}
	want := "--profile mimi chat -q card 73 is unsettled in Refined"
	if lines[0] != want {
		t.Fatalf("hermes invoked as %q, want %q", lines[0], want)
	}
}

func TestConcurrentWakesCollapseToOneRun(t *testing.T) {
	hermes, argsLog, dir := stubHermes(t)
	if err := os.WriteFile(filepath.Join(dir, "hold"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ended := make(chan string, 4)
	r := New(hermes, time.Minute, func(profile string) { ended <- profile })

	const wakes = 8
	var wg sync.WaitGroup
	ids := make([]string, wakes)
	sessions := make([]string, wakes)
	errs := make([]error, wakes)
	for i := 0; i < wakes; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i], sessions[i], errs[i] = r.Start(context.Background(), "mimi", "wake")
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if ids[i] != ids[0] || sessions[i] != sessions[0] {
			t.Fatalf("wake %d returned (%q, %q), want the one run (%q, %q)", i, ids[i], sessions[i], ids[0], sessions[0])
		}
	}
	if !r.Running("mimi") {
		t.Fatal("profile reads as not running while its run is held")
	}
	// Let the single child go, so the test leaks no process.
	if err := os.Remove(filepath.Join(dir, "hold")); err != nil {
		t.Fatal(err)
	}
	waitEnd(t, ended, "mimi")
	if lines := invocations(t, argsLog); len(lines) != 1 {
		t.Fatalf("hermes invoked %d times, want 1", len(lines))
	}
}

func TestDeadChildRecordsItsExitCode(t *testing.T) {
	hermes, _, dir := stubHermes(t)
	setExitCode(t, dir, 3)
	ended := make(chan string, 4)
	r := New(hermes, time.Minute, func(profile string) { ended <- profile })
	id, _, err := r.Start(context.Background(), "mimi", "wake")
	if err != nil {
		t.Fatal(err)
	}
	waitEnd(t, ended, "mimi")
	if r.Running("mimi") {
		t.Fatal("profile still reads as running after its child exited")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	tracked := r.table["mimi"]
	if tracked == nil || tracked.id != id {
		t.Fatalf("run table holds %+v, want the run %q", tracked, id)
	}
	if !tracked.exited || tracked.exitCode != 3 {
		t.Fatalf("run recorded exited=%v code=%d, want exited=true code=3", tracked.exited, tracked.exitCode)
	}
}

func TestRunPastItsBoundDies(t *testing.T) {
	hermes, _, dir := stubHermes(t)
	t.Setenv("STUB_SLEEP", "30")
	ended := make(chan string, 4)
	r := New(hermes, 200*time.Millisecond, func(profile string) { ended <- profile })
	start := time.Now()
	if _, _, err := r.Start(context.Background(), "mimi", "wake"); err != nil {
		t.Fatal(err)
	}
	waitEnd(t, ended, "mimi")
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Fatalf("bound run took %v, want it dead near its bound", elapsed)
	}
	if r.Running("mimi") {
		t.Fatal("profile still reads as running after its bound killed it")
	}
	// The bound kills the child's whole process group: a run past its bound
	// dies with the grandchild the stub hatched, not just the leader. Poll
	// the process table for a survivor of the group and fail on one.
	pgid := r.leader("mimi")
	deadline := time.Now().Add(5 * time.Second)
	for {
		survivors := groupSurvivors(t, pgid, dir)
		if len(survivors) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the bound left %d survivor(s) of the run's group: %v", len(survivors), survivors)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestTheRunChildLeadsItsOwnProcessGroup(t *testing.T) {
	hermes, _, dir := stubHermes(t)
	if err := os.WriteFile(filepath.Join(dir, "hold"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ended := make(chan string, 4)
	r := New(hermes, 0 /* no bound */, func(profile string) { ended <- profile })
	if _, _, err := r.Start(context.Background(), "mimi", "wake"); err != nil {
		t.Fatal(err)
	}
	// The child leads its own process group, which is what lets a kill to
	// the group reach the grandchild it hatched. Asserted while it is held
	// alive: a child that kept its parent's group reads as that group.
	leader := r.leader("mimi")
	if pgrp := childPgrp(t, leader); pgrp != leader {
		t.Fatalf("child %d runs in group %d, want it to lead its own group", leader, pgrp)
	}
	if err := os.Remove(filepath.Join(dir, "hold")); err != nil {
		t.Fatal(err)
	}
	waitEnd(t, ended, "mimi")
}

// leader is the run's process group id: the pid of the child that leads it.
func (r *Runner) leader(profile string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.table[profile].cmd.Process.Pid
}

// childPgrp is the process group a live child belongs to, straight from
// /proc. A child that kept its parent's group reads as that group, not its
// own pid.
func childPgrp(t *testing.T, pid int) (pgrp int) {
	t.Helper()
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		t.Fatal(err) // a child this test just started is always in /proc
	}
	stat := string(raw)
	fields := strings.Fields(stat[strings.LastIndexByte(stat, ')')+1:])
	pgrp, err = strconv.Atoi(fields[2]) // state, ppid, pgrp
	if err != nil {
		t.Fatal(err)
	}
	return pgrp
}

// groupSurvivors lists the processes still alive in the run's process group.
// A group the bound failed to kill keeps entries here after the onEnd that
// ended the test's wait: the leader gone, the survivors reparented but the
// group not. The group id is a fresh pid, so nothing else on the machine
// shares it; the command-line check keeps the count honest across pid reuse.
func groupSurvivors(t *testing.T, pgid int, dir string) (survivors []int) {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // not a pid directory
		}
		raw, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue // a process that ended between the two reads
		}
		stat := string(raw)
		fields := strings.Fields(stat[strings.LastIndexByte(stat, ')')+1:])
		pgrp, err := strconv.Atoi(fields[2]) // state, ppid, pgrp
		if err != nil || pgrp != pgid {
			continue
		}
		cmdline, err := os.ReadFile("/proc/" + entry.Name() + "/cmdline")
		if err != nil {
			continue
		}
		args := string(bytes.ReplaceAll(cmdline, []byte{0}, []byte{' '}))
		if strings.Contains(args, dir) || strings.HasPrefix(strings.TrimSpace(args), "sleep ") {
			survivors = append(survivors, pid)
		}
	}
	return survivors
}

func TestEveryRunEndFiresOnEndExactlyOnce(t *testing.T) {
	hermes, _, dir := stubHermes(t)
	setExitCode(t, dir, 1)
	ended := make(chan string, 4)
	r := New(hermes, time.Minute, func(profile string) { ended <- profile })
	if _, _, err := r.Start(context.Background(), "mimi", "first"); err != nil {
		t.Fatal(err)
	}
	waitEnd(t, ended, "mimi")
	if _, _, err := r.Start(context.Background(), "mimi", "second"); err != nil {
		t.Fatal(err)
	}
	waitEnd(t, ended, "mimi")
	select {
	case extra := <-ended:
		t.Fatalf("onEnd fired an extra time for %q", extra)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestAFailedStartFiresNoEnd(t *testing.T) {
	ended := make(chan string, 4)
	r := New(filepath.Join(t.TempDir(), "no-such-hermes"), time.Minute, func(profile string) { ended <- profile })
	if _, _, err := r.Start(context.Background(), "mimi", "wake"); err == nil {
		t.Fatal("Start with no binary succeeded, want an error")
	}
	if r.Running("mimi") {
		t.Fatal("profile reads as running after a start that failed")
	}
	select {
	case extra := <-ended:
		t.Fatalf("onEnd fired for %q without a run", extra)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestCancelledContextStartsNothing(t *testing.T) {
	hermes, argsLog, _ := stubHermes(t)
	ended := make(chan string, 4)
	r := New(hermes, time.Minute, func(profile string) { ended <- profile })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := r.Start(ctx, "mimi", "wake"); err == nil {
		t.Fatal("Start on a cancelled context succeeded, want an error")
	}
	if lines := invocations(t, argsLog); len(lines) != 0 {
		t.Fatalf("hermes invoked %d times, want none", len(lines))
	}
}
