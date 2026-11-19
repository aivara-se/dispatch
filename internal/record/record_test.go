package record_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aivara-se/dispatch/internal/record"
)

func TestEnsureJobCreatesOneJob(t *testing.T) {
	ctx := context.Background()
	store := open(t)

	job, created, err := store.EnsureJob(ctx, "review", "aivara-se", "dispatch", 212, "212")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !created {
		t.Fatal("first ensure did not report a new job")
	}
	if job.State != record.StateStarted || job.Verdict != record.VerdictPending || job.Key != "212" {
		t.Fatalf("job = %+v", job)
	}

	again, created, err := store.EnsureJob(ctx, "review", "aivara-se", "dispatch", 212, "changed")
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
			if _, _, err := store.EnsureJob(ctx, "review", "aivara-se", "dispatch", 7, "7"); err != nil {
				t.Fatal(err)
			}
			ended, err := store.EndJobs(ctx, "aivara-se", "dispatch", 7, tc.merged)
			if err != nil {
				t.Fatal(err)
			}
			if ended != 1 {
				t.Fatalf("ended = %d", ended)
			}
			job, err := store.Job(ctx, "review", "aivara-se", "dispatch", 7)
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
			if ended != 0 {
				t.Fatalf("second end = %d", ended)
			}
		})
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

func TestJobSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dispatch.db")
	first, err := record.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := first.EnsureJob(ctx, "review", "aivara-se", "dispatch", 9, "9"); err != nil {
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
	job, err := second.Job(ctx, "review", "aivara-se", "dispatch", 9)
	if err != nil {
		t.Fatalf("job after reopen: %v", err)
	}
	if job.Key != "9" || job.State != record.StateStarted {
		t.Fatalf("job = %+v", job)
	}
	if _, err := second.Job(ctx, "other", "aivara-se", "dispatch", 9); !errors.Is(err, record.ErrNotFound) {
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
