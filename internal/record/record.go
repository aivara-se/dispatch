// Package record keeps the engine's state in SQLite: deliveries, jobs, tasks
// and actions. It is the only writer.
package record

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// A job's state: started, or ended as blocked or done.
const (
	StateStarted = "started"
	StateBlocked = "blocked"
	StateDone    = "done"
)

// A job's verdict: pending, failed, or passed only at a success.
const (
	VerdictPending = "pending"
	VerdictFailed  = "failed"
	VerdictPassed  = "passed"
)

// ErrNotFound reports a job the record does not hold.
var ErrNotFound = errors.New("not found")

// Store is the open record.
type Store struct {
	db *sql.DB
}

// Delivery is one verified GitHub delivery kept for deduplication.
type Delivery struct {
	ID     string
	Kind   string
	Action string
	Owner  string
	Repo   string
	Number int
}

// Job is one flow's work on one pull request.
type Job struct {
	ID        int64
	Flow      string
	Owner     string
	Repo      string
	Number    int
	Key       string
	State     string
	Verdict   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Open opens or creates the SQLite record at path.
func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, err
	}
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     abs,
		RawQuery: "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
	}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, statement := range schema {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			return nil, fmt.Errorf("record: %w", err)
		}
	}
	return &Store{db: db}, nil
}

// Close closes the record.
func (s *Store) Close() error {
	return s.db.Close()
}

// HasDelivery reports whether id was already kept.
func (s *Store) HasDelivery(ctx context.Context, id string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM deliveries WHERE id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// RecordDelivery keeps a delivery once; a repeated id is refused by the key.
func (s *Store) RecordDelivery(ctx context.Context, d Delivery) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO deliveries (id, event, action, owner, repo, number, received_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.Kind, d.Action, d.Owner, d.Repo, d.Number, stamp(time.Now()))
	return err
}

// Job returns the job for a flow, repository and pull request number.
func (s *Store) Job(ctx context.Context, flow, owner, repo string, number int) (*Job, error) {
	return s.queryJob(ctx,
		`SELECT id, flow, owner, repo, number, key, state, verdict, created_at, updated_at
		 FROM jobs WHERE flow = ? AND owner = ? AND repo = ? AND number = ?`,
		flow, owner, repo, number)
}

// EnsureJob returns the job for a flow, repository and pull request number,
// and creates it with the generated key when it is absent.
func (s *Store) EnsureJob(ctx context.Context, flow, owner, repo string, number int, key string) (*Job, bool, error) {
	now := stamp(time.Now())
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (flow, owner, repo, number, key, state, verdict, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (flow, owner, repo, number) DO NOTHING`,
		flow, owner, repo, number, key, StateStarted, VerdictPending, now, now)
	if err != nil {
		return nil, false, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	job, err := s.Job(ctx, flow, owner, repo, number)
	if err != nil {
		return nil, false, err
	}
	return job, changed == 1, nil
}

// EndJobs ends every started job for a pull request: done and passed when it
// merged, blocked and failed when it closed unmerged. It returns how many
// jobs it ended.
func (s *Store) EndJobs(ctx context.Context, owner, repo string, number int, merged bool) (int64, error) {
	state, verdict := StateBlocked, VerdictFailed
	if merged {
		state, verdict = StateDone, VerdictPassed
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET state = ?, verdict = ?, updated_at = ?
		 WHERE owner = ? AND repo = ? AND number = ? AND state = ?`,
		state, verdict, stamp(time.Now()), owner, repo, number, StateStarted)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) queryJob(ctx context.Context, query string, args ...any) (*Job, error) {
	var job Job
	var created, updated string
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&job.ID, &job.Flow, &job.Owner, &job.Repo, &job.Number, &job.Key,
		&job.State, &job.Verdict, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if job.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return nil, err
	}
	if job.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return nil, err
	}
	return &job, nil
}

func stamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

var schema = []string{
	`CREATE TABLE IF NOT EXISTS deliveries (
		id TEXT PRIMARY KEY,
		event TEXT NOT NULL,
		action TEXT NOT NULL,
		owner TEXT NOT NULL,
		repo TEXT NOT NULL,
		number INTEGER NOT NULL,
		received_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS jobs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		flow TEXT NOT NULL,
		owner TEXT NOT NULL,
		repo TEXT NOT NULL,
		number INTEGER NOT NULL,
		key TEXT NOT NULL,
		state TEXT NOT NULL,
		verdict TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE (flow, owner, repo, number)
	)`,
	`CREATE TABLE IF NOT EXISTS tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		job_id INTEGER NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		n INTEGER NOT NULL,
		state TEXT NOT NULL,
		evidence TEXT,
		started_at TEXT,
		ended_at TEXT,
		UNIQUE (job_id, n)
	)`,
	`CREATE TABLE IF NOT EXISTS actions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id INTEGER NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
		ordinal INTEGER NOT NULL,
		type TEXT NOT NULL,
		agent TEXT,
		text TEXT,
		path TEXT,
		args TEXT,
		name TEXT,
		value TEXT,
		output TEXT,
		exit INTEGER,
		started_at TEXT,
		ended_at TEXT,
		UNIQUE (task_id, ordinal)
	)`,
}
