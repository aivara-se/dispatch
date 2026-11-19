// Package runner runs one job's tasks: the actions in order, then the next
// task its conditions name.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aivara-se/dispatch/internal/config"
	"github.com/aivara-se/dispatch/internal/hermes"
	"github.com/aivara-se/dispatch/internal/record"
)

// Launch runs one prompt action.
type Launch interface {
	Run(ctx context.Context, profile, text, key string) (hermes.Result, error)
}

// Conditions reads a task's next conditions.
type Conditions interface {
	Head(ctx context.Context, owner, repo string, number int) (string, error)
	Holds(ctx context.Context, key, owner, repo string, number int, startHead string) (bool, json.RawMessage, error)
}

// Labeler writes the blocked label.
type Labeler interface {
	Block(ctx context.Context, owner, repo string, number int) error
}

type identity struct {
	flow   string
	owner  string
	repo   string
	number int
}

// Runner runs jobs, one task at a time, one goroutine per job.
type Runner struct {
	store      *record.Store
	flows      map[string]config.Flow
	configDir  string
	agents     map[string]string
	launch     Launch
	conditions Conditions
	labels     Labeler
	publish    func(flow, owner, repo, key string)

	mu      sync.Mutex
	running map[identity]context.CancelFunc
}

// New returns a runner over the configured flows and the record.
func New(store *record.Store, flows []config.Flow, configDir string, agents map[string]string,
	launch Launch, conditions Conditions, labels Labeler, publish func(flow, owner, repo, key string)) *Runner {
	byName := make(map[string]config.Flow, len(flows))
	for _, flow := range flows {
		byName[flow.Name] = flow
	}
	return &Runner{
		store: store, flows: byName, configDir: configDir, agents: agents,
		launch: launch, conditions: conditions, labels: labels, publish: publish,
		running: map[identity]context.CancelFunc{},
	}
}

// Start runs a job in the background. A job already running is left alone.
func (r *Runner) Start(job record.Job) {
	id := identity{flow: job.Flow, owner: job.Owner, repo: job.Repo, number: job.Number}
	r.mu.Lock()
	if _, ok := r.running[id]; ok {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.running[id] = cancel
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.running, id)
			r.mu.Unlock()
		}()
		r.run(ctx, job)
	}()
}

// Kill stops every job of one pull request.
func (r *Runner) Kill(owner, repo string, number int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, cancel := range r.running {
		if id.owner == owner && id.repo == repo && id.number == number {
			cancel()
		}
	}
}

// Close stops every running job.
func (r *Runner) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, cancel := range r.running {
		cancel()
	}
}

// Resume starts every job the record left unfinished.
func (r *Runner) Resume(ctx context.Context) error {
	jobs, err := r.store.UnfinishedJobs(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		r.Start(job)
	}
	return nil
}

func (r *Runner) run(ctx context.Context, job record.Job) {
	for {
		if ctx.Err() != nil {
			return
		}
		flow, ok := r.flows[job.Flow]
		if !ok {
			r.stop(ctx, job, "unknown flow "+job.Flow)
			return
		}
		if job.Task == "_done_" {
			r.end(ctx, job)
			return
		}
		task, ok := flow.Tasks[job.Task]
		if !ok {
			r.stop(ctx, job, "unknown task "+job.Task)
			return
		}

		rows, err := r.store.Tasks(ctx, job.ID)
		if err != nil {
			r.stop(ctx, job, err.Error())
			return
		}
		current := byID(rows, job.TaskID)
		if current == nil {
			runs, err := r.store.RunCount(ctx, job.ID, job.Task)
			if err != nil {
				r.stop(ctx, job, err.Error())
				return
			}
			if runs >= task.Limit.Turn {
				r.stop(ctx, job, "limit.turn reached")
				return
			}
			head, err := r.conditions.Head(ctx, job.Owner, job.Repo, job.Number)
			if err != nil {
				r.stop(ctx, job, err.Error())
				return
			}
			begun, err := r.store.BeginTask(ctx, job.ID, job.Task, head)
			if err != nil {
				r.stop(ctx, job, err.Error())
				return
			}
			if err := r.store.SetTaskID(ctx, job.ID, begun.ID); err != nil {
				r.stop(ctx, job, err.Error())
				return
			}
			job.TaskID = begun.ID
			current = &begun
		}
		if current.State == record.TaskFailed {
			r.stop(ctx, job, "task failed")
			return
		}

		if current.State == record.TaskRunning {
			tools, failed, err := r.actions(ctx, job, task, *current)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				r.stop(ctx, job, err.Error())
				return
			}
			if failed {
				if _, err := r.store.FinishTask(ctx, current.ID, record.TaskFailed, "", "", tools); err != nil {
					r.stop(ctx, job, err.Error())
					return
				}
				r.stop(ctx, job, "an action failed")
				return
			}
			current.Tools = tools
			current.State = record.TaskDone
		}

		target, pc, evidence, holds, err := r.next(ctx, job, task, current.Head)
		if err != nil {
			_, _ = r.store.FinishTask(ctx, current.ID, record.TaskFailed, pc, evidence, current.Tools)
			r.stop(ctx, job, err.Error())
			return
		}
		changed, err := r.store.FinishTask(ctx, current.ID, record.TaskDone, pc, evidence, current.Tools)
		if err != nil {
			r.stop(ctx, job, err.Error())
			return
		}
		if !changed {
			// A close interrupted this task while it ran, so the record is no
			// longer ours to advance.
			return
		}
		r.announce(job)
		if !holds {
			r.stop(ctx, job, "no condition holds")
			return
		}
		if err := r.store.SetTask(ctx, job.ID, target, 0); err != nil {
			r.stop(ctx, job, err.Error())
			return
		}
		job.Task = target
		job.TaskID = 0
		if target == "_done_" {
			r.end(ctx, job)
			return
		}
	}
}

// actions runs the task's actions in order, resuming after the last finished
// one. It returns the merged tool calls and whether an action failed.
func (r *Runner) actions(ctx context.Context, job record.Job, task config.Task, current record.Task) (string, bool, error) {
	taskCtx, cancel := context.WithTimeout(ctx, time.Duration(task.Limit.Time)*time.Second)
	defer cancel()

	tools := current.Tools
	for i, action := range task.Actions {
		existing := at(current.Actions, i)
		if existing != nil && (existing.Exit != nil || existing.Skipped) {
			continue
		}
		if ctx.Err() != nil {
			return tools, false, ctx.Err()
		}
		actionID, err := r.store.BeginAction(ctx, current.ID, i, record.Action{
			Type: action.Type, Agent: action.Agent, Text: action.Text,
			Path: action.Path, Args: action.Args, Name: action.Name, Value: action.Value,
		})
		if err != nil {
			return tools, false, err
		}
		output, exit, actionTools, err := r.action(taskCtx, job, action)
		if ctx.Err() != nil {
			return tools, false, ctx.Err()
		}
		if taskCtx.Err() != nil {
			r.store.FinishAction(context.Background(), actionID, output, -1)
			return tools, true, nil
		}
		if err != nil {
			r.store.FinishAction(context.Background(), actionID, output, 1)
			return tools, true, nil
		}
		if exit != 0 && output == "" && err == nil {
			output = "exit " + strconv.Itoa(exit)
		}
		if err := r.store.FinishAction(ctx, actionID, output, exit); err != nil {
			return tools, false, err
		}
		tools = merge(tools, actionTools)
		if exit != 0 {
			for j := i + 1; j < len(task.Actions); j++ {
				next := task.Actions[j]
				r.store.SkipAction(context.Background(), current.ID, j, record.Action{
					Type: next.Type, Agent: next.Agent, Text: next.Text,
					Path: next.Path, Args: next.Args, Name: next.Name, Value: next.Value,
				})
			}
			return tools, true, nil
		}
	}
	return tools, false, nil
}

func (r *Runner) action(ctx context.Context, job record.Job, action config.Action) (string, int, string, error) {
	switch action.Type {
	case "prompt":
		profile, ok := r.agents[action.Agent]
		if !ok {
			return "", 1, "", fmt.Errorf("agent %q is not configured", action.Agent)
		}
		text := strings.ReplaceAll(action.Text, "{pr.url}", job.Link)
		result, err := r.launch.Run(ctx, profile, text, job.Identity())
		if err != nil {
			return result.Output, result.Exit, result.Tools, err
		}
		return result.Output, result.Exit, result.Tools, nil
	case "script":
		command := exec.CommandContext(ctx, filepath.Join(r.configDir, action.Path), action.Args...)
		output, err := command.CombinedOutput()
		if err == nil {
			return string(output), 0, "", nil
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return string(output), exit.ExitCode(), "", nil
		}
		if ctx.Err() != nil {
			return string(output), -1, "", ctx.Err()
		}
		return string(output), 1, "", nil
	case "action":
		if action.Name != "label" {
			return "", 1, "", fmt.Errorf("unknown action %q", action.Name)
		}
		if err := r.labels.Block(ctx, job.Owner, job.Repo, job.Number); err != nil {
			return err.Error(), 1, "", nil
		}
		return "", 0, "", nil
	default:
		return "", 1, "", fmt.Errorf("unknown action type %q", action.Type)
	}
}

// next returns the first condition that holds, with its reading.
func (r *Runner) next(ctx context.Context, job record.Job, task config.Task, head string) (string, string, string, bool, error) {
	pc, evidence := "", ""
	for _, condition := range task.Next {
		holds, read, err := r.conditions.Holds(ctx, condition.Key, job.Owner, job.Repo, job.Number, head)
		if err != nil {
			return "", pc, evidence, false, err
		}
		pc = fmt.Sprintf(`{"expr":%q,"want":true,"got":%t}`, condition.Key, holds)
		evidence = string(read)
		if holds {
			return condition.Target, pc, evidence, true, nil
		}
	}
	return "", pc, evidence, false, nil
}

func (r *Runner) end(ctx context.Context, job record.Job) {
	if err := r.store.EndJob(ctx, job.ID, record.StateDone, record.VerdictPassed); err != nil {
		return
	}
	job.State, job.Verdict = record.StateDone, record.VerdictPassed
	r.announce(job)
}

// stop ends a job as blocked and writes the label the engine owns. It runs
// before the state change, so a reader that sees blocked also sees the label.
func (r *Runner) stop(ctx context.Context, job record.Job, alert string) {
	current, err := r.store.Job(ctx, job.Flow, job.Owner, job.Repo, job.Number)
	if err != nil || current.State != record.StateStarted {
		return
	}
	job.Alert = alert
	r.store.SetJobAlert(ctx, job.ID, alert)
	if err := r.labels.Block(ctx, job.Owner, job.Repo, job.Number); err != nil {
		job.Alert = alert + "; blocked label: " + err.Error()
		r.store.SetJobAlert(ctx, job.ID, job.Alert)
	}
	if err := r.store.EndJob(ctx, job.ID, record.StateBlocked, record.VerdictFailed); err != nil {
		return
	}
	job.State, job.Verdict = record.StateBlocked, record.VerdictFailed
	r.announce(job)
}

func (r *Runner) announce(job record.Job) {
	if r.publish != nil {
		r.publish(job.Flow, job.Owner, job.Repo, job.Key)
	}
}

func byID(rows []record.Task, id int64) *record.Task {
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

func at(actions []record.Action, ordinal int) *record.Action {
	if ordinal < len(actions) {
		return &actions[ordinal]
	}
	return nil
}

// merge joins two JSON arrays of tool calls.
func merge(current, next string) string {
	if next == "" {
		return current
	}
	if current == "" {
		return next
	}
	var a, b []json.RawMessage
	if json.Unmarshal([]byte(current), &a) != nil || json.Unmarshal([]byte(next), &b) != nil {
		return next
	}
	body, err := json.Marshal(append(a, b...))
	if err != nil {
		return next
	}
	return string(body)
}
