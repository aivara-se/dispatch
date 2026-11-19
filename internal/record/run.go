package record

import (
	"context"
	"encoding/json"
	"time"
)

// SetTask points a job at its current task. A zero taskID means the next run
// begins a new task row.
func (s *Store) SetTask(ctx context.Context, jobID int64, task string, taskID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET task = ?, task_id = ?, updated_at = ? WHERE id = ? AND state = ?`,
		task, taskID, stamp(time.Now()), jobID, StateStarted)
	return err
}

// SetTaskID records the task row a job is running.
func (s *Store) SetTaskID(ctx context.Context, jobID, taskID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET task_id = ?, updated_at = ? WHERE id = ? AND state = ?`,
		taskID, stamp(time.Now()), jobID, StateStarted)
	return err
}

// EndJob writes a job's final state once; a job that already ended keeps it.
func (s *Store) EndJob(ctx context.Context, jobID int64, state, verdict string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET state = ?, verdict = ?, updated_at = ? WHERE id = ? AND state = ?`,
		state, verdict, stamp(time.Now()), jobID, StateStarted)
	return err
}

// SetJobAlert records a message on one job.
func (s *Store) SetJobAlert(ctx context.Context, jobID int64, message string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET alert = ?, updated_at = ? WHERE id = ?`, message, stamp(time.Now()), jobID)
	return err
}

// UnfinishedJobs lists the jobs a restart must resume.
func (s *Store) UnfinishedJobs(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, jobColumns+` WHERE state = ? ORDER BY id`, StateStarted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanJobs(rows)
}

// RunCount is how many times a task has run in a job.
func (s *Store) RunCount(ctx context.Context, jobID int64, name string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE job_id = ? AND name = ?`, jobID, name).Scan(&count)
	return count, err
}

// BeginTask creates the next task row and returns it.
func (s *Store) BeginTask(ctx context.Context, jobID int64, name, head string) (Task, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(n), 0) + 1 FROM tasks WHERE job_id = ?`, jobID).Scan(&n); err != nil {
		return Task{}, err
	}
	at := stamp(time.Now())
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (job_id, name, n, state, head, started_at) VALUES (?, ?, ?, ?, ?, ?)`,
		jobID, name, n, TaskRunning, head, at)
	if err != nil {
		return Task{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Task{}, err
	}
	return Task{ID: id, N: n, Short: name, State: TaskRunning, Head: head, At: at}, nil
}

// FinishTask writes a task's state, the condition that decided it and its
// evidence. An empty tools value keeps what an earlier run wrote.
func (s *Store) FinishTask(ctx context.Context, taskID int64, state, pc, evidence, tools string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET state = ?, pc = ?, evidence = ?, tools = COALESCE(NULLIF(?, ''), tools), ended_at = ?
		 WHERE id = ?`,
		state, pc, evidence, tools, stamp(time.Now()), taskID)
	return err
}

// BeginAction records an action before it runs, so a crash leaves it pending.
func (s *Store) BeginAction(ctx context.Context, taskID int64, ordinal int, action Action) (int64, error) {
	args, err := json.Marshal(action.Args)
	if err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO actions (task_id, ordinal, type, agent, text, path, args, name, value, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		taskID, ordinal, action.Type, action.Agent, action.Text, action.Path, string(args),
		action.Name, action.Value, stamp(time.Now()))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishAction writes the action's output and exit.
func (s *Store) FinishAction(ctx context.Context, actionID int64, output string, exit int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE actions SET output = ?, exit = ?, ended_at = ? WHERE id = ?`,
		output, exit, stamp(time.Now()), actionID)
	return err
}

// SkipAction records an action the task did not run.
func (s *Store) SkipAction(ctx context.Context, taskID int64, ordinal int, action Action) error {
	args, err := json.Marshal(action.Args)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO actions (task_id, ordinal, type, agent, text, path, args, name, value, skipped, ended_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
		taskID, ordinal, action.Type, action.Agent, action.Text, action.Path, string(args),
		action.Name, action.Value, stamp(time.Now()))
	return err
}
