// Package runner runs one job's tasks: the actions in order, then the next
// task its conditions name.
package runner

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/aivara-se/dispatch/internal/config"
	"github.com/aivara-se/dispatch/internal/hermes"
	"github.com/aivara-se/dispatch/internal/record"
)

type Launch interface {
	Run(ctx context.Context, profile, text, key string) (hermes.Result, error)
}

type Conditions interface {
	Head(ctx context.Context, owner, repo string, number int) (string, error)
	Holds(ctx context.Context, key, owner, repo string, number int, startHead string) (bool, json.RawMessage, error)
}

type Labeler interface {
	Block(ctx context.Context, owner, repo string, number int) error
}

// Publisher announces a job that changed, so the stream's readers refetch it.
type Publisher func(flow, owner, repo, key string)

// identity is the job's place in the record, and the key of the goroutine
// running it.
type identity struct {
	flow   string
	owner  string
	repo   string
	number int
	agent  string
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
	publish    Publisher
	handlers   map[string]actionFunc

	mu      sync.Mutex
	running map[identity]context.CancelFunc
}

// New returns a runner over the configured flows and the record.
func New(store *record.Store, flows []config.Flow, configDir string, agents map[string]string,
	launch Launch, conditions Conditions, labels Labeler, publish Publisher) *Runner {
	byName := make(map[string]config.Flow, len(flows))
	for _, flow := range flows {
		byName[flow.Name] = flow
	}
	r := &Runner{
		store: store, flows: byName, configDir: configDir, agents: agents,
		launch: launch, conditions: conditions, labels: labels, publish: publish,
		running: map[identity]context.CancelFunc{},
	}
	r.handlers = map[string]actionFunc{
		config.ActionPrompt: r.prompt,
		config.ActionScript: r.script,
		config.ActionNamed:  r.named,
	}
	return r
}

// Start runs a job in the background. A job already running is left alone.
func (r *Runner) Start(job record.Job) {
	id := identity{flow: job.Flow, owner: job.Owner, repo: job.Repo, number: job.Number, agent: job.Agent}
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

// run advances one job until it ends, blocks, or loses the record to a close.
func (r *Runner) run(ctx context.Context, job record.Job) {
	for {
		if ctx.Err() != nil {
			return
		}
		if job.Task == config.TaskDone {
			r.end(ctx, job)
			return
		}
		next, ok := r.step(ctx, &job)
		if !ok {
			return
		}
		job.Task, job.TaskID = next, 0
		if next == config.TaskDone {
			r.end(ctx, job)
			return
		}
	}
}

// step runs the job's current task and returns the task its conditions name.
// It reports false when the job ended, or when the record is no longer the
// runner's to advance.
func (r *Runner) step(ctx context.Context, job *record.Job) (string, bool) {
	flow, ok := r.flows[job.Flow]
	if !ok {
		r.stop(ctx, *job, "unknown flow "+job.Flow)
		return "", false
	}
	task, ok := flow.Tasks[job.Task]
	if !ok {
		r.stop(ctx, *job, "unknown task "+job.Task)
		return "", false
	}

	current, ok := r.begin(ctx, job, task)
	if !ok {
		return "", false
	}
	if current.State == record.TaskFailed {
		r.stop(ctx, *job, "task failed")
		return "", false
	}
	if current.State == record.TaskRunning {
		tools, failed, err := r.perform(ctx, *job, task, *current)
		if ctx.Err() != nil {
			return "", false
		}
		if err != nil {
			r.stop(ctx, *job, err.Error())
			return "", false
		}
		if failed {
			if _, err := r.store.FinishTask(ctx, current.ID, record.TaskFailed, "", "", tools); err != nil {
				r.stop(ctx, *job, err.Error())
				return "", false
			}
			r.stop(ctx, *job, "an action failed")
			return "", false
		}
		current.Tools, current.State = tools, record.TaskDone
	}

	target, condition, evidence, holds, err := r.decide(ctx, *job, task, current.Head)
	if err != nil {
		_, _ = r.store.FinishTask(ctx, current.ID, record.TaskFailed, condition, evidence, current.Tools)
		r.stop(ctx, *job, err.Error())
		return "", false
	}
	changed, err := r.store.FinishTask(ctx, current.ID, record.TaskDone, condition, evidence, current.Tools)
	if err != nil {
		r.stop(ctx, *job, err.Error())
		return "", false
	}
	if !changed {
		return "", false
	}
	r.announce(*job)
	if !holds {
		r.stop(ctx, *job, "no condition holds")
		return "", false
	}
	if err := r.store.SetTask(ctx, job.ID, target, 0); err != nil {
		r.stop(ctx, *job, err.Error())
		return "", false
	}
	return target, true
}

// end passes a job that reached the end of its flow.
func (r *Runner) end(ctx context.Context, job record.Job) {
	if err := r.store.EndJob(ctx, job.ID, record.StateDone, record.VerdictPassed); err != nil {
		return
	}
	job.State, job.Verdict = record.StateDone, record.VerdictPassed
	r.announce(job)
}

// stop ends a job as blocked and writes the label the engine owns. The label
// runs before the state change, so a reader that sees blocked also sees it,
// and a rejected label is kept on the job as part of its alert.
func (r *Runner) stop(ctx context.Context, job record.Job, alert string) {
	current, err := r.store.Job(ctx, job.Flow, job.Owner, job.Repo, job.Number, job.Agent)
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
