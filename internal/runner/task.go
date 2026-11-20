package runner

import (
	"context"
	"fmt"

	"github.com/aivara-se/dispatch/internal/config"
	"github.com/aivara-se/dispatch/internal/record"
)

// begin returns the task row the job runs, opening one when the job has none
// and its budget allows another run.
func (r *Runner) begin(ctx context.Context, job *record.Job, task config.Task) (*record.Task, bool) {
	rows, err := r.store.Tasks(ctx, job.ID)
	if err != nil {
		r.stop(ctx, *job, err.Error())
		return nil, false
	}
	if current := taskByID(rows, job.TaskID); current != nil {
		return current, true
	}

	runs, err := r.store.RunCount(ctx, job.ID, job.Task)
	if err != nil {
		r.stop(ctx, *job, err.Error())
		return nil, false
	}
	if runs >= task.Limit.Turn {
		r.stop(ctx, *job, "limit.turn reached")
		return nil, false
	}
	head, err := r.conditions.Head(ctx, job.Owner, job.Repo, job.Number)
	if err != nil {
		r.stop(ctx, *job, err.Error())
		return nil, false
	}
	begun, err := r.store.BeginTask(ctx, job.ID, job.Task, head)
	if err != nil {
		r.stop(ctx, *job, err.Error())
		return nil, false
	}
	if err := r.store.SetTaskID(ctx, job.ID, begun.ID); err != nil {
		r.stop(ctx, *job, err.Error())
		return nil, false
	}
	job.TaskID = begun.ID
	return &begun, true
}

// decide returns the first condition that holds, with its target and the read
// that decided it.
func (r *Runner) decide(ctx context.Context, job record.Job, task config.Task, head string) (string, string, string, bool, error) {
	condition, evidence := "", ""
	for _, next := range task.Next {
		holds, read, err := r.conditions.Holds(ctx, next.Key, job.Owner, job.Repo, job.Number, head)
		if err != nil {
			return "", condition, evidence, false, err
		}
		condition = fmt.Sprintf(`{"expr":%q,"want":true,"got":%t}`, next.Key, holds)
		evidence = string(read)
		if holds {
			return next.Target, condition, evidence, true, nil
		}
	}
	return "", condition, evidence, false, nil
}

func taskByID(rows []record.Task, id int64) *record.Task {
	if id == 0 {
		return nil
	}
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i]
		}
	}
	return nil
}
