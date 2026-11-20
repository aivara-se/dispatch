// Package record keeps the engine's state in SQLite: deliveries, jobs, tasks
// and actions. It is the only writer.
package record

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound reports a job the record does not hold.
var ErrNotFound = errors.New("not found")

// SchemaVersion is the record's shape. A process whose own version is older
// than the file's stops at boot instead of reading a shape it does not know.
const SchemaVersion = 3

// Store is the open record.
type Store struct {
	db   *sql.DB
	path string
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

func (s *Store) Close() error {
	return s.db.Close()
}

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
	for _, column := range addedColumns {
		if err := s.ensureColumn(column.table, column.name, column.decl); err != nil {
			return err
		}
	}
	if err := s.rebuildJobs(); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE jobs SET verdict = ? WHERE state = ? AND verdict = ?`,
		VerdictPassed, StateDone, VerdictPending); err != nil {
		return err
	}
	return s.setVersion(SchemaVersion)
}

// addedColumns are the columns a record written by an earlier build lacks.
// A merged close ends its job passed (docs/adrs/001), so rows written before
// the close path wrote a verdict carry done and pending together: a pair no
// writer produces now, and one a reader cannot act on.
var addedColumns = []struct {
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

func (s *Store) ensureColumn(table, name, decl string) error {
	found, err := s.hasColumn(table, name)
	if err != nil || found {
		return err
	}
	_, err = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + name + ` ` + decl)
	return err
}

// rebuild copies the jobs table into a shape that carries the agent, then
// renames it into place. A dropped parent table would cascade into its tasks,
// so the rebuild runs with foreign keys off.
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

func stamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
