package record_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aivara-se/dispatch/internal/record"
)

func TestEnsureJobCreatesOneJob(t *testing.T) {
	ctx := context.Background()
	store := open(t)

	job, created, err := store.EnsureJob(ctx, start("review", 212, "212"))
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !created {
		t.Fatal("first ensure did not report a new job")
	}
	if job.State != record.StateStarted || job.Verdict != record.VerdictPending || job.Key != "212" {
		t.Fatalf("job = %+v", job)
	}

	again, created, err := store.EnsureJob(ctx, start("review", 212, "changed"))
	if err != nil {
		t.Fatalf("ensure again: %v", err)
	}
	if created {
		t.Fatal("second ensure reported a new job")
	}
	if again.ID != job.ID {
		t.Fatalf("ids = %d and %d", job.ID, again.ID)
	}
	if again.Key != "212" {
		t.Fatalf("stored key = %q, want the first write", again.Key)
	}
}

func TestIdentity(t *testing.T) {
	job := record.Job{Flow: "review", Owner: "aivara-se", Repo: "dispatch", Key: "212"}
	if got := job.Identity(); got != "review/aivara-se/dispatch/212" {
		t.Fatalf("identity = %q", got)
	}
}

func TestEndJobs(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		merged  bool
		state   string
		verdict string
	}{
		{name: "unmerged", merged: false, state: record.StateBlocked, verdict: record.VerdictFailed},
		{name: "merged", merged: true, state: record.StateDone, verdict: record.VerdictPassed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := open(t)
			if _, _, err := store.EnsureJob(ctx, start("review", 7, "7")); err != nil {
				t.Fatal(err)
			}
			ended, err := store.EndJobs(ctx, "aivara-se", "dispatch", 7, tc.merged)
			if err != nil {
				t.Fatal(err)
			}
			if len(ended) != 1 {
				t.Fatalf("ended = %d", len(ended))
			}
			job, err := store.Job(ctx, "review", "aivara-se", "dispatch", 7, "")
			if err != nil {
				t.Fatal(err)
			}
			if job.State != tc.state || job.Verdict != tc.verdict {
				t.Fatalf("job = %s/%s", job.State, job.Verdict)
			}
			ended, err = store.EndJobs(ctx, "aivara-se", "dispatch", 7, tc.merged)
			if err != nil {
				t.Fatal(err)
			}
			if len(ended) != 0 {
				t.Fatalf("second end = %d", len(ended))
			}
		})
	}
}

func TestMergedClosePassesTheJob(t *testing.T) {
	// ADR 036: the merge is the effect the flow wanted, so the close passes.
	ctx := context.Background()
	store := open(t)
	if _, _, err := store.EnsureJob(ctx, start("review", 7, "7")); err != nil {
		t.Fatal(err)
	}
	ended, err := store.EndJobs(ctx, "aivara-se", "dispatch", 7, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ended) != 1 || ended[0].State != record.StateDone || ended[0].Verdict != record.VerdictPassed {
		t.Fatalf("ended = %+v", ended)
	}
	job, err := store.Job(ctx, "review", "aivara-se", "dispatch", 7, "")
	if err != nil {
		t.Fatal(err)
	}
	if job.State != record.StateDone || job.Verdict != record.VerdictPassed {
		t.Fatalf("job = %s/%s", job.State, job.Verdict)
	}
}

func TestMergedCloseEndsItsRunningTask(t *testing.T) {
	// A close kills the task in flight. The record ends it interrupted, with
	// its end written, so a done job never answers beside a running task.
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dispatch.db")
	store, err := record.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, _, err := store.EnsureJob(ctx, start("review", 7, "7"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.BeginTask(ctx, job.ID, "merge", "s1")
	if err != nil {
		t.Fatal(err)
	}
	actionID, err := store.BeginAction(ctx, task.ID, 0, record.Action{Type: "prompt", Agent: "meme", Text: "Merge."})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.EndJobs(ctx, "aivara-se", "dispatch", 7, true); err != nil {
		t.Fatal(err)
	}

	tasks, err := store.Tasks(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].State != record.TaskInterrupted {
		t.Fatalf("tasks = %+v", tasks)
	}

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var endedAt sql.NullString
	if err := db.QueryRow(`SELECT ended_at FROM tasks WHERE id = ?`, task.ID).Scan(&endedAt); err != nil {
		t.Fatal(err)
	}
	if !endedAt.Valid || endedAt.String == "" {
		t.Fatal("the interrupted task recorded no end")
	}
	var exit sql.NullInt64
	if err := db.QueryRow(`SELECT exit FROM actions WHERE id = ?`, actionID).Scan(&exit); err != nil {
		t.Fatal(err)
	}
	if exit.Valid {
		t.Fatalf("a killed action recorded exit %d", exit.Int64)
	}
}

func TestALateWriteKeepsAnInterruptedTask(t *testing.T) {
	// ADR 036:15-16: the close wins. The runner's tail that arrives after it
	// changes nothing, so the task keeps interrupted and its action no exit.
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dispatch.db")
	store, err := record.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, _, err := store.EnsureJob(ctx, start("review", 7, "7"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.BeginTask(ctx, job.ID, "merge", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginAction(ctx, task.ID, 0, record.Action{Type: "prompt", Agent: "meme", Text: "Merge."}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EndJobs(ctx, "aivara-se", "dispatch", 7, true); err != nil {
		t.Fatal(err)
	}

	changed, err := store.FinishTask(ctx, task.ID, record.TaskDone, "merged", "s1", "")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("a late write ended the interrupted task")
	}
	tasks, err := store.Tasks(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].State != record.TaskInterrupted {
		t.Fatalf("tasks = %+v", tasks)
	}
}

func TestAnInterruptedActionKeepsNoExit(t *testing.T) {
	// ADR 036:16: a killed action keeps no exit, so the write that lands after
	// the close changes nothing.
	ctx := context.Background()
	store := open(t)
	job, _, err := store.EnsureJob(ctx, start("review", 7, "7"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.BeginTask(ctx, job.ID, "merge", "s1")
	if err != nil {
		t.Fatal(err)
	}
	actionID, err := store.BeginAction(ctx, task.ID, 0, record.Action{Type: "prompt", Agent: "meme", Text: "Merge."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EndJobs(ctx, "aivara-se", "dispatch", 7, true); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishAction(ctx, actionID, "merged", 0); err != nil {
		t.Fatal(err)
	}

	tasks, err := store.Tasks(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || len(tasks[0].Actions) != 1 {
		t.Fatalf("tasks = %+v", tasks)
	}
	if action := tasks[0].Actions[0]; action.Exit != nil || action.Output != "" {
		t.Fatalf("action = %+v", action)
	}
}

func TestRecordDeliveryKeepsOneID(t *testing.T) {
	ctx := context.Background()
	store := open(t)
	delivery := record.Delivery{ID: "d1", Kind: "pull_request", Action: "opened", Owner: "aivara-se", Repo: "dispatch", Number: 1}
	if err := store.RecordDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	seen, err := store.HasDelivery(ctx, "d1")
	if err != nil || !seen {
		t.Fatalf("seen = %v, err = %v", seen, err)
	}
	if err := store.RecordDelivery(ctx, delivery); err == nil {
		t.Fatal("second delivery id was stored")
	}
}

func TestOpenMigratesAnM0Record(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dispatch.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE jobs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		flow TEXT NOT NULL, owner TEXT NOT NULL, repo TEXT NOT NULL, number INTEGER NOT NULL,
		key TEXT NOT NULL, state TEXT NOT NULL, verdict TEXT NOT NULL,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
		UNIQUE (flow, owner, repo, number))`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO jobs (flow, owner, repo, number, key, state, verdict, created_at, updated_at)
		VALUES ('review', 'aivara-se', 'dispatch', 212, '212', 'started', 'pending',
		'2026-11-19T00:00:00Z', '2026-11-19T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := record.Open(path)
	if err != nil {
		t.Fatalf("open an m0 record: %v", err)
	}
	defer store.Close()
	job, err := store.Job(context.Background(), "review", "aivara-se", "dispatch", 212, "")
	if err != nil {
		t.Fatal(err)
	}
	if job.Key != "212" || job.Title != "" || job.Alert != "" {
		t.Fatalf("job = %+v", job)
	}
}

func TestJobSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dispatch.db")
	first, err := record.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := first.EnsureJob(ctx, start("review", 9, "9")); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := record.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	job, err := second.Job(ctx, "review", "aivara-se", "dispatch", 9, "")
	if err != nil {
		t.Fatalf("job after reopen: %v", err)
	}
	if job.Key != "9" || job.State != record.StateStarted {
		t.Fatalf("job = %+v", job)
	}
	if _, err := second.Job(ctx, "other", "aivara-se", "dispatch", 9, ""); !errors.Is(err, record.ErrNotFound) {
		t.Fatalf("unknown job error = %v", err)
	}
}

func open(t *testing.T) *record.Store {
	t.Helper()
	store, err := record.Open(filepath.Join(t.TempDir(), "dispatch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func start(flow string, number int, key string) record.Start {
	return record.Start{Flow: flow, Owner: "aivara-se", Repo: "dispatch", Number: number, Key: key}
}

// A record written before the agent joined a job's identity is rebuilt on
// open: every job, task and action survives, and a second agent's job for the
// same pull request can then be written.
func TestOpenRebuildsTheJobsTableKeepingTasks(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dispatch.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			flow TEXT NOT NULL, owner TEXT NOT NULL, repo TEXT NOT NULL, number INTEGER NOT NULL,
			key TEXT NOT NULL, title TEXT NOT NULL DEFAULT '', link TEXT NOT NULL DEFAULT '', alert TEXT,
			task TEXT NOT NULL DEFAULT '', task_id INTEGER NOT NULL DEFAULT 0,
			state TEXT NOT NULL, verdict TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
			UNIQUE (flow, owner, repo, number))`,
		`CREATE TABLE tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id INTEGER NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
			name TEXT NOT NULL, n INTEGER NOT NULL, state TEXT NOT NULL,
			pc TEXT, evidence TEXT, tools TEXT, head TEXT,
			started_at TEXT, ended_at TEXT,
			UNIQUE (job_id, n))`,
		`CREATE TABLE actions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id INTEGER NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL, type TEXT NOT NULL, agent TEXT, text TEXT, path TEXT, args TEXT,
			name TEXT, value TEXT, output TEXT, exit INTEGER, skipped INTEGER NOT NULL DEFAULT 0,
			started_at TEXT, ended_at TEXT,
			UNIQUE (task_id, ordinal))`,
		`INSERT INTO jobs (id, flow, owner, repo, number, key, title, task, state, verdict, created_at, updated_at)
		 VALUES (7, 'review', 'aivara-se', 'dispatch', 212, '212', 'a title', 'review', 'started', 'pending',
		 '2026-11-19T00:00:00Z', '2026-11-19T00:00:00Z')`,
		`INSERT INTO tasks (id, job_id, name, n, state, head) VALUES (3, 7, 'review', 1, 'running', 'sha1')`,
		`INSERT INTO actions (task_id, ordinal, type, agent, text) VALUES (3, 0, 'prompt', 'memo', 'Review {pr.url}.')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%v: %v", err, statement)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := record.Open(path)
	if err != nil {
		t.Fatalf("open the old record: %v", err)
	}
	defer store.Close()

	job, err := store.Job(ctx, "review", "aivara-se", "dispatch", 212, "")
	if err != nil {
		t.Fatalf("job after the rebuild: %v", err)
	}
	if job.ID != 7 || job.Key != "212" || job.Title != "a title" || job.Agent != "" {
		t.Fatalf("job = %+v", job)
	}
	tasks, err := store.Tasks(ctx, job.ID)
	if err != nil {
		t.Fatalf("tasks after the rebuild: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != 3 || len(tasks[0].Actions) != 1 {
		t.Fatalf("the rebuild lost a task: %+v", tasks)
	}
	if tasks[0].Actions[0].Agent != "memo" {
		t.Fatalf("action = %+v", tasks[0].Actions[0])
	}
	second, created, err := store.EnsureJob(ctx, record.Start{
		Flow: "review", Owner: "aivara-se", Repo: "dispatch", Number: 212,
		Key: "212-momo", Agent: "momo", Task: "review",
	})
	if err != nil {
		t.Fatalf("a second agent's job: %v", err)
	}
	if !created || second.ID == job.ID || second.Agent != "momo" {
		t.Fatalf("second job = %+v, created = %v", second, created)
	}
}

func TestOpenRefusesANewerRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dispatch.db")
	store, err := record.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = record.Open(path)
	if err == nil {
		t.Fatal("a newer record opened")
	}
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "99") || !strings.Contains(err.Error(), "2") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("the refused open removed the record: %v", statErr)
	}
}

func TestOpenRaisesTheVersionOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dispatch.db")
	for i := 0; i < 2; i++ {
		store, err := record.Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite", "file:"+path)
		if err != nil {
			t.Fatal(err)
		}
		var version int
		if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
			t.Fatal(err)
		}
		db.Close()
		if version != record.SchemaVersion {
			t.Fatalf("version = %d, want %d", version, record.SchemaVersion)
		}
	}
}
