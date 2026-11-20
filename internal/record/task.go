package record

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// A task's state: running, or ended as done, failed or interrupted.
const (
	TaskRunning     = "running"
	TaskDone        = "done"
	TaskFailed      = "failed"
	TaskInterrupted = "interrupted"
)

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
		task.Head = head.String
		task.At = at.String
		task.PC = pc.String
		task.Evidence = evidence.String
		task.Tools = tools.String
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
		action.Agent = agent.String
		action.Text = text.String
		action.Path = path.String
		action.Name = name.String
		action.Value = value.String
		action.Output = output.String
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
// evidence. An empty tools value keeps what an earlier run wrote. A task that
// another writer ended keeps its row, so a close that interrupted a task wins
// over the runner's tail; the report says whether the write landed.
func (s *Store) FinishTask(ctx context.Context, taskID int64, state, pc, evidence, tools string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET state = ?, pc = ?, evidence = ?, tools = COALESCE(NULLIF(?, ''), tools), ended_at = ?
		 WHERE id = ? AND state IN (?, ?)`,
		state, pc, evidence, tools, stamp(time.Now()), taskID, state, TaskRunning)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

// BeginAction records an action before it runs, so a crash leaves it pending.
// A resumed task re-begins the action its crash left pending, so the row is
// the one it already has, not a second one.
func (s *Store) BeginAction(ctx context.Context, taskID int64, ordinal int, action Action) (int64, error) {
	args, err := json.Marshal(action.Args)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO actions (task_id, ordinal, type, agent, text, path, args, name, value, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (task_id, ordinal) DO UPDATE SET started_at = excluded.started_at
		 RETURNING id`,
		taskID, ordinal, action.Type, action.Agent, action.Text, action.Path, string(args),
		action.Name, action.Value, stamp(time.Now())).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// FinishAction writes the action's output and exit. An action whose task a
// close ended keeps no exit, so the write is scoped to a running task.
func (s *Store) FinishAction(ctx context.Context, actionID int64, output string, exit int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE actions SET output = ?, exit = ?, ended_at = ?
		 WHERE id = ? AND (SELECT state FROM tasks WHERE tasks.id = actions.task_id) = ?`,
		output, exit, stamp(time.Now()), actionID, TaskRunning)
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
