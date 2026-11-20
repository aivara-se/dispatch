// Package record keeps the engine's state in SQLite: deliveries, jobs, tasks
// and actions. It is the only writer.
package record

import (
	"context"
	"database/sql"
	"encoding/json"
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

// A job's verdict: pending, failed, or passed at a success.
const (
	VerdictPending = "pending"
	VerdictFailed  = "failed"
	VerdictPassed  = "passed"
)

// A task's state: running, or ended as done, failed or interrupted.
const (
	TaskRunning     = "running"
	TaskDone        = "done"
	TaskFailed      = "failed"
	TaskInterrupted = "interrupted"
)

// ErrNotFound reports a job the record does not hold.
var ErrNotFound = errors.New("not found")

// SchemaVersion is the record's shape. A process whose own version is older
// than the file's stops at boot instead of reading a shape it does not know.
const SchemaVersion = 2

// Store is the open record.
type Store struct {
	db   *sql.DB
	path string
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

// Identity is the job's path: flow/owner/repo/key.
func (j Job) Identity() string {
	return j.Flow + "/" + j.Owner + "/" + j.Repo + "/" + j.Key
}

// Task is one task of a job, with the actions it ran.
type Task struct {
	ID       int64
	N        int
	Short    string
	State    string
	Head     string
	At       string
	Actions  []Action
	PC       string
	Evidence string
	Tools    string
}

// Action is one step a task ran.
type Action struct {
	Type    string
	Agent   string
	Text    string
	Path    string
	Args    []string
	Name    string
	Value   string
	Output  string
	Exit    *int
	Skipped bool
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
	store := &Store{db: db, path: abs}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
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
// ended. A merged close writes done and passed (docs/adrs/036); an unmerged
// close writes blocked and failed (docs/adrs/029). A task still running under
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

// Tasks returns a job's tasks in order, each with its actions.
func (s *Store) Tasks(ctx context.Context, jobID int64) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, n, name, state, head, started_at, pc, evidence, tools FROM tasks WHERE job_id = ? ORDER BY n`, jobID)
	if err != nil {
		return nil, err
	}
	type row struct {
		id   int64
		task Task
	}
	var rowsOut []row
	for rows.Next() {
		var id int64
		var task Task
		var head, at, pc, evidence, tools sql.NullString
		if err := rows.Scan(&id, &task.N, &task.Short, &task.State, &head, &at, &pc, &evidence, &tools); err != nil {
			rows.Close()
			return nil, err
		}
		task.ID = id
		task.Head = stringOf(head)
		task.At = stringOf(at)
		task.PC = stringOf(pc)
		task.Evidence = stringOf(evidence)
		task.Tools = stringOf(tools)
		rowsOut = append(rowsOut, row{id: id, task: task})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	tasks := make([]Task, 0, len(rowsOut))
	for _, item := range rowsOut {
		actions, err := s.actions(ctx, item.id)
		if err != nil {
			return nil, err
		}
		item.task.Actions = actions
		tasks = append(tasks, item.task)
	}
	return tasks, nil
}

func (s *Store) actions(ctx context.Context, taskID int64) ([]Action, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT type, agent, text, path, args, name, value, output, exit, skipped
		 FROM actions WHERE task_id = ? ORDER BY ordinal`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var actions []Action
	for rows.Next() {
		var action Action
		var agent, text, path, args, name, value, output sql.NullString
		var exit sql.NullInt64
		var skipped int
		if err := rows.Scan(&action.Type, &agent, &text, &path, &args, &name, &value, &output, &exit, &skipped); err != nil {
			return nil, err
		}
		action.Agent = stringOf(agent)
		action.Text = stringOf(text)
		action.Path = stringOf(path)
		action.Name = stringOf(name)
		action.Value = stringOf(value)
		action.Output = stringOf(output)
		action.Skipped = skipped == 1
		if args.Valid && args.String != "" {
			if err := json.Unmarshal([]byte(args.String), &action.Args); err != nil {
				return nil, err
			}
		}
		if exit.Valid {
			value := int(exit.Int64)
			action.Exit = &value
		}
		actions = append(actions, action)
	}
	return actions, rows.Err()
}

func (s *Store) queryJob(ctx context.Context, query string, args ...any) (*Job, error) {
	var job Job
	var alert sql.NullString
	var created, updated string
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&job.ID, &job.Flow, &job.Owner, &job.Repo, &job.Number, &job.Key, &job.Agent,
		&job.Title, &job.Link, &alert, &job.Task, &job.TaskID, &job.State, &job.Verdict, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	job.Alert = alert.String
	if job.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return nil, err
	}
	if job.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return nil, err
	}
	return &job, nil
}

func scanJobs(rows *sql.Rows) ([]Job, error) {
	var jobs []Job
	for rows.Next() {
		var job Job
		var alert sql.NullString
		var created, updated string
		if err := rows.Scan(
			&job.ID, &job.Flow, &job.Owner, &job.Repo, &job.Number, &job.Key, &job.Agent,
			&job.Title, &job.Link, &alert, &job.Task, &job.TaskID, &job.State, &job.Verdict, &created, &updated); err != nil {
			return nil, err
		}
		job.Alert = alert.String
		var err error
		if job.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, err
		}
		if job.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func stringOf(value sql.NullString) string {
	return value.String
}

func stamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// jobFields is the column list every job read returns, in scan order.
const jobFields = `id, flow, owner, repo, number, key, agent, title, link, alert, task, task_id, state, verdict, created_at, updated_at`

const jobColumns = `SELECT ` + jobFields + ` FROM jobs`

// migrate raises an older record to SchemaVersion. A record newer than this
// build stops before a delivery can meet a shape it cannot read.
func (s *Store) migrate() error {
	version, err := s.version()
	if err != nil {
		return err
	}
	if version > SchemaVersion {
		return fmt.Errorf("record %s: schema version %d, this build reads %d",
			s.path, version, SchemaVersion)
	}
	for _, statement := range schema {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("record: %w", err)
		}
	}
	columns := []struct {
		table string
		name  string
		decl  string
	}{
		{"jobs", "title", `TEXT NOT NULL DEFAULT ''`},
		{"jobs", "link", `TEXT NOT NULL DEFAULT ''`},
		{"jobs", "alert", `TEXT`},
		{"jobs", "task", `TEXT NOT NULL DEFAULT ''`},
		{"jobs", "task_id", `INTEGER NOT NULL DEFAULT 0`},
		{"tasks", "pc", `TEXT`},
		{"tasks", "tools", `TEXT`},
		{"tasks", "head", `TEXT`},
		{"actions", "skipped", `INTEGER NOT NULL DEFAULT 0`},
	}
	for _, column := range columns {
		if err := s.ensureColumn(column.table, column.name, column.decl); err != nil {
			return err
		}
	}
	if err := s.rebuildJobs(); err != nil {
		return err
	}
	return s.setVersion(SchemaVersion)
}

// version is the record's schema version. A file this build's migration has
// not raised yet reads 0.
func (s *Store) version() (int, error) {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func (s *Store) setVersion(version int) error {
	_, err := s.db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version))
	return err
}

// rebuildJobs copies the jobs table when a job's identity predates the agent:
// a unique constraint cannot be altered, so the table is rebuilt. The copy
// keeps every row and every task, and a fresh record never enters it.
func (s *Store) rebuildJobs() error {
	agent, err := s.hasColumn("jobs", "agent")
	if err != nil || agent {
		return err
	}
	// A dropped parent table would cascade into its tasks, so the rebuild runs
	// with foreign keys off; the pragma is a no-op inside a transaction.
	if _, err := s.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer s.db.Exec(`PRAGMA foreign_keys = ON`)

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range rebuild {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("record: rebuild jobs: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) hasColumn(table, name string) (bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var column, kind string
		var notNull, primary int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &column, &kind, &notNull, &dflt, &primary); err != nil {
			return false, err
		}
		if column == name {
			return true, nil
		}
	}
	return false, rows.Err()
}

// rebuild copies the jobs table into a shape that carries the agent, then
// renames it into place.
var rebuild = []string{
	`CREATE TABLE jobs_rebuild (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		flow TEXT NOT NULL,
		owner TEXT NOT NULL,
		repo TEXT NOT NULL,
		number INTEGER NOT NULL,
		key TEXT NOT NULL,
		agent TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		link TEXT NOT NULL DEFAULT '',
		alert TEXT,
		task TEXT NOT NULL DEFAULT '',
		task_id INTEGER NOT NULL DEFAULT 0,
		state TEXT NOT NULL,
		verdict TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE (flow, owner, repo, number, agent)
	)`,
	`INSERT INTO jobs_rebuild
		(id, flow, owner, repo, number, key, agent, title, link, alert, task, task_id, state, verdict, created_at, updated_at)
		SELECT id, flow, owner, repo, number, key, '', title, link, alert, task, task_id, state, verdict, created_at, updated_at
		FROM jobs`,
	`DROP TABLE jobs`,
	`ALTER TABLE jobs_rebuild RENAME TO jobs`,
}

func (s *Store) ensureColumn(table, name, decl string) error {
	found, err := s.hasColumn(table, name)
	if err != nil || found {
		return err
	}
	_, err = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + name + ` ` + decl)
	return err
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
		agent TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		link TEXT NOT NULL DEFAULT '',
		alert TEXT,
		task TEXT NOT NULL DEFAULT '',
		task_id INTEGER NOT NULL DEFAULT 0,
		state TEXT NOT NULL,
		verdict TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE (flow, owner, repo, number, agent)
	)`,
	`CREATE TABLE IF NOT EXISTS tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		job_id INTEGER NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		n INTEGER NOT NULL,
		state TEXT NOT NULL,
		pc TEXT,
		evidence TEXT,
		tools TEXT,
		head TEXT,
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
		skipped INTEGER NOT NULL DEFAULT 0,
		started_at TEXT,
		ended_at TEXT,
		UNIQUE (task_id, ordinal)
	)`,
}
