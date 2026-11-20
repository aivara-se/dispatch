package record

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// A job's state: started, or ended as blocked or done.
const (
	StateStarted = "started"
	StateBlocked = "blocked"
	StateDone    = "done"
)

// A job's verdict: pending, failed, or passed at a success.
const (
	VerdictPending = "pending"
	VerdictFailed  = "failed"
	VerdictPassed  = "passed"
)

// Delivery is one verified GitHub delivery kept for deduplication.
type Delivery struct {
	ID     string
	Kind   string
	Action string
	Owner  string
	Repo   string
	Number int
}

// Start is the job a flow starts on a delivery.
type Start struct {
	Flow   string
	Owner  string
	Repo   string
	Number int
	Key    string
	Agent  string
	Title  string
	Link   string
	Task   string
}

// Job is one flow's work on one pull request.
type Job struct {
	ID        int64
	Flow      string
	Owner     string
	Repo      string
	Number    int
	Key       string
	Agent     string
	Title     string
	Link      string
	Alert     string
	Task      string
	TaskID    int64
	State     string
	Verdict   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Identity is the job's path: flow/owner/repo/key. It is the session a job's
// prompts resume, so it carries the agent through the key.
func (j Job) Identity() string {
	return j.Flow + "/" + j.Owner + "/" + j.Repo + "/" + j.Key
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

// Job returns the job for a flow, repository, pull request number and agent.
// A flow that starts once per pull request keeps its job under the empty agent.
func (s *Store) Job(ctx context.Context, flow, owner, repo string, number int, agent string) (*Job, error) {
	return s.queryJob(ctx, jobColumns+` WHERE flow = ? AND owner = ? AND repo = ? AND number = ? AND agent = ?`,
		flow, owner, repo, number, agent)
}

// JobByKey returns the job a key names within a flow and repository.
func (s *Store) JobByKey(ctx context.Context, flow, owner, repo, key string) (*Job, error) {
	return s.queryJob(ctx, jobColumns+` WHERE flow = ? AND owner = ? AND repo = ? AND key = ?`,
		flow, owner, repo, key)
}

// Jobs returns the jobs of one flow, most recently changed first.
func (s *Store) Jobs(ctx context.Context, flow string) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, jobColumns+` WHERE flow = ? ORDER BY updated_at DESC, id DESC`, flow)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanJobs(rows)
}

// AllJobs returns every job, most recently changed first.
func (s *Store) AllJobs(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, jobColumns+` ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanJobs(rows)
}

// FlowCounts returns the number of jobs per flow.
func (s *Store) FlowCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT flow, COUNT(*) FROM jobs GROUP BY flow`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var flow string
		var count int
		if err := rows.Scan(&flow, &count); err != nil {
			return nil, err
		}
		counts[flow] = count
	}
	return counts, rows.Err()
}

// EnsureJob returns the job for a flow, repository and pull request number,
// and creates it with the generated key when it is absent.
func (s *Store) EnsureJob(ctx context.Context, start Start) (*Job, bool, error) {
	now := stamp(time.Now())
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (flow, owner, repo, number, key, agent, title, link, task, state, verdict, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (flow, owner, repo, number, agent) DO NOTHING`,
		start.Flow, start.Owner, start.Repo, start.Number, start.Key, start.Agent, start.Title, start.Link,
		start.Task, StateStarted, VerdictPending, now, now)
	if err != nil {
		return nil, false, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	job, err := s.Job(ctx, start.Flow, start.Owner, start.Repo, start.Number, start.Agent)
	if err != nil {
		return nil, false, err
	}
	return job, changed == 1, nil
}

// EndJobs ends every started job for a pull request and returns the jobs it
// ended. A merged close writes done and passed (docs/adrs/001); an unmerged
// close writes blocked and failed (docs/adrs/001). A task still running under
// an ended job is interrupted, with its end recorded, so a done job never
// shows a task in progress.
func (s *Store) EndJobs(ctx context.Context, owner, repo string, number int, merged bool) ([]Job, error) {
	state, verdict := StateBlocked, VerdictFailed
	if merged {
		state, verdict = StateDone, VerdictPassed
	}
	now := stamp(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`UPDATE tasks SET state = ?, ended_at = ?
		 WHERE state = ? AND job_id IN (
		     SELECT id FROM jobs WHERE owner = ? AND repo = ? AND number = ? AND state = ?)`,
		TaskInterrupted, now, TaskRunning, owner, repo, number, StateStarted); err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx,
		`UPDATE jobs SET state = ?, verdict = ?, updated_at = ?
		 WHERE owner = ? AND repo = ? AND number = ? AND state = ?
		 RETURNING `+jobFields,
		state, verdict, now, owner, repo, number, StateStarted)
	if err != nil {
		return nil, err
	}
	jobs, err := scanJobs(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return jobs, nil
}

// SetAlert records a message on every job for a pull request.
func (s *Store) SetAlert(ctx context.Context, owner, repo string, number int, message string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET alert = ?, updated_at = ? WHERE owner = ? AND repo = ? AND number = ?`,
		message, stamp(time.Now()), owner, repo, number)
	return err
}

// rowScanner is what *sql.Row and *sql.Rows share, so one scan reads a job
// from either.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (Job, error) {
	var job Job
	var alert sql.NullString
	var created, updated string
	if err := row.Scan(
		&job.ID, &job.Flow, &job.Owner, &job.Repo, &job.Number, &job.Key, &job.Agent,
		&job.Title, &job.Link, &alert, &job.Task, &job.TaskID, &job.State, &job.Verdict, &created, &updated); err != nil {
		return Job{}, err
	}
	job.Alert = alert.String
	var err error
	if job.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return Job{}, err
	}
	if job.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return Job{}, err
	}
	return job, nil
}

func scanJobs(rows *sql.Rows) ([]Job, error) {
	var jobs []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) queryJob(ctx context.Context, query string, args ...any) (*Job, error) {
	job, err := scanJob(s.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// jobFields is the column list every job read returns, in scan order.
const jobFields = `id, flow, owner, repo, number, key, agent, title, link, alert, task, task_id, state, verdict, created_at, updated_at`

const jobColumns = `SELECT ` + jobFields + ` FROM jobs`
