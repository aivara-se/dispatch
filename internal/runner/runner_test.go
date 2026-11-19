package runner_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aivara-se/dispatch/internal/config"
	"github.com/aivara-se/dispatch/internal/hermes"
	"github.com/aivara-se/dispatch/internal/record"
	"github.com/aivara-se/dispatch/internal/runner"
)

type call struct {
	profile string
	text    string
	session string
}

type launch struct {
	mu      sync.Mutex
	calls   []call
	output  string
	exit    int
	tools   string
	err     error
	block   bool
	release chan struct{}
	started chan struct{}
	done    chan struct{}
}

func (l *launch) Run(ctx context.Context, profile, text, session string) (hermes.Result, error) {
	l.mu.Lock()
	l.calls = append(l.calls, call{profile: profile, text: text, session: session})
	l.mu.Unlock()
	if l.started != nil {
		select {
		case l.started <- struct{}{}:
		default:
		}
	}
	if l.block {
		<-ctx.Done()
		if l.done != nil {
			close(l.done)
		}
		return hermes.Result{}, ctx.Err()
	}
	if l.release != nil {
		<-l.release
	}
	return hermes.Result{Output: l.output, Exit: l.exit, Tools: l.tools}, l.err
}

func (l *launch) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.calls)
}

type conditions struct {
	head  string
	holds map[string]bool
}

func (c *conditions) Head(context.Context, string, string, int) (string, error) {
	return c.head, nil
}

func (c *conditions) Holds(_ context.Context, key, _ string, _ string, _ int, _ string) (bool, json.RawMessage, error) {
	return c.holds[key], json.RawMessage(`{"read":true}`), nil
}

type labeler struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (l *labeler) Block(context.Context, string, string, int) error {
	l.mu.Lock()
	l.calls++
	l.mu.Unlock()
	return l.err
}

func (l *labeler) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

func TestRunsActionsInOrder(t *testing.T) {
	store, job, r, launch, labels := newHarness(t, "review", "review", "212", &conditions{
		head:  "s1",
		holds: map[string]bool{"pr.approved": true},
	})
	launch.output = "reviewed"
	launch.tools = `[{"name":"read"}]`

	r.Start(job)
	ended := wait(t, store, "review")

	if ended.State != record.StateDone || ended.Verdict != record.VerdictPassed {
		t.Fatalf("job = %s/%s", ended.State, ended.Verdict)
	}
	tasks, err := store.Tasks(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	if tasks[0].State != "done" || tasks[0].Head != "s1" || len(tasks[0].Actions) != 3 {
		t.Fatalf("task = %+v", tasks[0])
	}
	wants := []struct {
		output string
		exit   int
	}{
		{"reviewed", 0},
		{"checked --fast", 0},
		{"", 0},
	}
	for i, want := range wants {
		action := tasks[0].Actions[i]
		if action.Output != want.output || action.Exit == nil || *action.Exit != want.exit {
			t.Fatalf("action %d = %+v", i, action)
		}
	}
	if tasks[0].Tools != `[{"name":"read"}]` {
		t.Fatalf("tools = %s", tasks[0].Tools)
	}
	if len(launch.calls) != 1 {
		t.Fatalf("launches = %d", len(launch.calls))
	}
	if launch.calls[0].profile != "momo" || launch.calls[0].text != "Review https://github.com/aivara-se/dispatch/pull/212." || launch.calls[0].session != "review/aivara-se/dispatch/212" {
		t.Fatalf("call = %+v", launch.calls[0])
	}
	if labels.count() != 1 {
		t.Fatalf("label calls = %d", labels.count())
	}
}

func TestFirstConditionWins(t *testing.T) {
	store, job, r, launch, _ := newHarness(t, "review", "review", "212", &conditions{
		head:  "s1",
		holds: map[string]bool{"pr.approved": true, "pr.rejected": true},
	})
	r.Start(job)
	ended := wait(t, store, "review")
	if ended.State != record.StateDone || ended.Verdict != record.VerdictPassed {
		t.Fatalf("job = %s/%s", ended.State, ended.Verdict)
	}
	if launch.count() != 1 {
		t.Fatalf("the second target ran: %d", launch.count())
	}
}

func TestFailedActionStopsAndSkips(t *testing.T) {
	store, job, r, launch, labels := newHarness(t, "review", "review", "212", &conditions{
		head:  "s1",
		holds: map[string]bool{"pr.approved": true},
	})
	launch.exit = 1
	launch.output = "boom"

	r.Start(job)
	ended := wait(t, store, "review")
	if ended.State != record.StateBlocked || ended.Verdict != record.VerdictFailed {
		t.Fatalf("job = %s/%s", ended.State, ended.Verdict)
	}
	tasks, _ := store.Tasks(context.Background(), job.ID)
	actions := tasks[0].Actions
	if actions[0].Exit == nil || *actions[0].Exit != 1 || actions[0].Output != "boom" {
		t.Fatalf("first action = %+v", actions[0])
	}
	if !actions[1].Skipped || !actions[2].Skipped {
		t.Fatalf("later actions = %+v", actions[1:])
	}
	if labels.count() != 1 {
		t.Fatalf("label calls = %d", labels.count())
	}
	if !strings.Contains(ended.Alert, "action failed") {
		t.Fatalf("alert = %q", ended.Alert)
	}
}

func TestDeadlineStopsTheJob(t *testing.T) {
	store, job, r, launch, _ := newHarness(t, "single", "run", "single-212", &conditions{head: "s1"})
	launch.block = true

	start := time.Now()
	r.Start(job)
	ended := wait(t, store, "single")
	if ended.State != record.StateBlocked {
		t.Fatalf("job = %s/%s", ended.State, ended.Verdict)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatalf("the deadline took %s", time.Since(start))
	}
	tasks, _ := store.Tasks(context.Background(), job.ID)
	if tasks[0].Actions[0].Exit == nil || *tasks[0].Actions[0].Exit != -1 {
		t.Fatalf("action = %+v", tasks[0].Actions[0])
	}
}

func TestNoConditionHoldsBlocks(t *testing.T) {
	store, job, r, _, _ := newHarness(t, "review", "review", "212", &conditions{head: "s1"})
	r.Start(job)
	ended := wait(t, store, "review")
	if ended.State != record.StateBlocked || ended.Verdict != record.VerdictFailed {
		t.Fatalf("job = %s/%s", ended.State, ended.Verdict)
	}
	if !strings.Contains(ended.Alert, "no condition holds") {
		t.Fatalf("alert = %q", ended.Alert)
	}
	tasks, _ := store.Tasks(context.Background(), job.ID)
	if !strings.Contains(tasks[0].PC, "pr.rejected") {
		t.Fatalf("pc = %s", tasks[0].PC)
	}
}

func TestAFailedLabelKeepsTheStopReason(t *testing.T) {
	store, job, r, _, labels := newHarness(t, "single", "run", "single-212", &conditions{head: "s1"})
	labels.err = errors.New("403 Forbidden")

	r.Start(job)
	ended := wait(t, store, "single")
	if ended.State != record.StateBlocked || ended.Verdict != record.VerdictFailed {
		t.Fatalf("job = %s/%s", ended.State, ended.Verdict)
	}
	if !strings.Contains(ended.Alert, "no condition holds") || !strings.Contains(ended.Alert, "403 Forbidden") {
		t.Fatalf("alert = %q", ended.Alert)
	}
}

func TestTwoRepositoriesShareNoSession(t *testing.T) {
	store, job, r, launch, _ := newHarness(t, "single", "run", "single-212", &conditions{
		head:  "s1",
		holds: map[string]bool{"pr.approved": true},
	})
	r.Start(job)
	wait(t, store, "single")

	other, _, err := store.EnsureJob(context.Background(), record.Start{
		Flow: "single", Owner: "aivara-se", Repo: "keysmash", Number: 212, Key: "single-212",
		Title: "another", Link: "https://github.com/aivara-se/keysmash/pull/212", Task: "run",
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Start(*other)
	waitRepo(t, store, "single", "keysmash")

	if len(launch.calls) != 2 {
		t.Fatalf("launches = %d", len(launch.calls))
	}
	if launch.calls[0].session == launch.calls[1].session {
		t.Fatalf("both jobs resume %q", launch.calls[0].session)
	}
}

func TestLimitTurnStops(t *testing.T) {
	store, job, r, _, _ := newHarness(t, "loop", "run", "loop-212", &conditions{
		head:  "s1",
		holds: map[string]bool{"pr.updated": true},
	})
	r.Start(job)
	ended := wait(t, store, "loop")
	if ended.State != record.StateBlocked {
		t.Fatalf("job = %s/%s", ended.State, ended.Verdict)
	}
	tasks, _ := store.Tasks(context.Background(), job.ID)
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	if !strings.Contains(ended.Alert, "limit.turn") {
		t.Fatalf("alert = %q", ended.Alert)
	}
}

func TestResumeSkipsFinishedActions(t *testing.T) {
	store, job, r, launch, _ := newHarness(t, "single", "run", "single-212", &conditions{
		head:  "s1",
		holds: map[string]bool{"pr.approved": true},
	})
	begun, err := store.BeginTask(context.Background(), job.ID, "run", "s0")
	if err != nil {
		t.Fatal(err)
	}
	actionID, err := store.BeginAction(context.Background(), begun.ID, 0, record.Action{Type: "prompt", Agent: "memo", Text: "Go."})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishAction(context.Background(), actionID, "already ran", 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SetTaskID(context.Background(), job.ID, begun.ID); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.Job(context.Background(), "single", "aivara-se", "dispatch", 212)
	if err != nil {
		t.Fatal(err)
	}
	job = *reloaded

	r.Start(job)
	ended := wait(t, store, "single")
	if ended.State != record.StateDone || ended.Verdict != record.VerdictPassed {
		t.Fatalf("job = %s/%s", ended.State, ended.Verdict)
	}
	if launch.count() != 0 {
		t.Fatalf("the finished action reran: %d", launch.count())
	}
	tasks, _ := store.Tasks(context.Background(), job.ID)
	if len(tasks) != 1 || tasks[0].State != "done" {
		t.Fatalf("tasks = %+v", tasks)
	}
}

func TestKillStopsTheAction(t *testing.T) {
	store, job, r, launch, _ := newHarness(t, "single", "run", "single-212", &conditions{head: "s1"})
	launch.block = true
	launch.started = make(chan struct{}, 1)
	launch.done = make(chan struct{})

	r.Start(job)
	select {
	case <-launch.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the action did not start")
	}
	r.Kill("aivara-se", "dispatch", 212)
	select {
	case <-launch.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the action was not killed")
	}
	time.Sleep(50 * time.Millisecond)
	current, err := store.Job(context.Background(), "single", "aivara-se", "dispatch", 212)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != record.StateStarted {
		t.Fatalf("job = %s", current.State)
	}
}

func TestACloseKeepsTheTaskItInterrupted(t *testing.T) {
	// The close lands while the action runs, so no cancellation saves the
	// record: the tail must not write over the interrupted task, nor advance
	// a job the close ended.
	store, job, r, launch, _ := newHarness(t, "single", "run", "single-212", &conditions{
		head:  "s1",
		holds: map[string]bool{"pr.approved": true},
	})
	launch.release = make(chan struct{})
	launch.started = make(chan struct{}, 1)

	r.Start(job)
	select {
	case <-launch.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the action did not start")
	}
	if _, err := store.EndJobs(context.Background(), "aivara-se", "dispatch", 212, true); err != nil {
		t.Fatal(err)
	}
	close(launch.release)
	time.Sleep(200 * time.Millisecond)

	ended, err := store.Job(context.Background(), "single", "aivara-se", "dispatch", 212)
	if err != nil {
		t.Fatal(err)
	}
	if ended.State != record.StateDone || ended.Verdict != record.VerdictPassed {
		t.Fatalf("job = %s/%s", ended.State, ended.Verdict)
	}
	if ended.Task != "run" {
		t.Fatalf("the runner advanced a job it lost to %q", ended.Task)
	}
	tasks, err := store.Tasks(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].State != record.TaskInterrupted {
		t.Fatalf("tasks = %+v", tasks)
	}
	for _, action := range tasks[0].Actions {
		if action.Exit != nil {
			t.Fatalf("the interrupted action recorded exit %d", *action.Exit)
		}
	}
}

func TestATaskDoneBeforeTheCrashFinishesOnResume(t *testing.T) {
	// FinishTask wrote done and the crash came before SetTask. The resumed job
	// finishes that task again, so the write must land on the done row.
	store, job, r, launch, _ := newHarness(t, "single", "run", "single-212", &conditions{
		head:  "s1",
		holds: map[string]bool{"pr.approved": true},
	})
	begun, err := store.BeginTask(context.Background(), job.ID, "run", "s0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishTask(context.Background(), begun.ID, record.TaskDone, "{}", "{}", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SetTaskID(context.Background(), job.ID, begun.ID); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.Job(context.Background(), "single", "aivara-se", "dispatch", 212)
	if err != nil {
		t.Fatal(err)
	}

	r.Start(*reloaded)
	ended := wait(t, store, "single")
	if ended.State != record.StateDone || ended.Verdict != record.VerdictPassed {
		t.Fatalf("job = %s/%s", ended.State, ended.Verdict)
	}
	if launch.count() != 0 {
		t.Fatalf("the finished task reran its action: %d", launch.count())
	}
}

func newHarness(t *testing.T, flowName, taskName, key string, cond *conditions) (*record.Store, record.Job, *runner.Runner, *launch, *labeler) {
	t.Helper()
	_, flows, err := config.Load("testdata")
	if err != nil {
		t.Fatal(err)
	}
	store, err := record.Open(filepath.Join(t.TempDir(), "dispatch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	job, _, err := store.EnsureJob(context.Background(), record.Start{
		Flow: flowName, Owner: "aivara-se", Repo: "dispatch", Number: 212, Key: key,
		Title: "a title", Link: "https://github.com/aivara-se/dispatch/pull/212", Task: taskName,
	})
	if err != nil {
		t.Fatal(err)
	}
	launcher := &launch{}
	labels := &labeler{}
	r := runner.New(store, flows, "testdata",
		map[string]string{"memo": "memo", "momo": "momo"}, launcher, cond, labels, nil)
	t.Cleanup(r.Close)
	return store, *job, r, launcher, labels
}

func wait(t *testing.T, store *record.Store, flow string) record.Job {
	t.Helper()
	return waitRepo(t, store, flow, "dispatch")
}

func waitRepo(t *testing.T, store *record.Store, flow, repo string) record.Job {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		job, err := store.Job(context.Background(), flow, "aivara-se", repo, 212)
		if err == nil && job.State != record.StateStarted {
			return *job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the job did not end")
	return record.Job{}
}
