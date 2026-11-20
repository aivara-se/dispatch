// Package engine turns a verified delivery into jobs: it starts the job each
// flow's start condition names, and ends a pull request's jobs when it closes.
// It runs no task.
package engine

import (
	"context"
	"sync"

	"github.com/aivara-se/dispatch/internal/config"
	"github.com/aivara-se/dispatch/internal/record"
	"github.com/aivara-se/dispatch/internal/webhook"
)

type Labeler interface {
	Block(ctx context.Context, owner, repo string, number int) error
}

// Publisher announces a job that changed, so the stream's readers refetch it.
type Publisher func(flow, owner, repo, key string)

// Jobs starts and stops the runner for a job.
type Jobs interface {
	Start(job record.Job)
	Kill(owner, repo string, number int)
}

// Engine applies deliveries to the record.
type Engine struct {
	mu      sync.Mutex
	store   *record.Store
	served  scope
	flows   []config.Flow
	agents  map[string]config.Agent
	labeler Labeler
	publish Publisher
	jobs    Jobs
}

// New returns an engine over one record, the served repositories, the
// configured flows, the agents a login names, the App's label writer, the
// stream's publisher and the job runner.
func New(store *record.Store, repos []string, flows []config.Flow, agents map[string]config.Agent,
	labeler Labeler, publish Publisher, jobs Jobs) *Engine {
	return &Engine{store: store, served: serve(repos), flows: flows, agents: agents,
		labeler: labeler, publish: publish, jobs: jobs}
}

// Deliver applies one verified delivery. A delivery that is already kept, is
// outside the served repositories, or starts nothing changes nothing.
func (e *Engine) Deliver(ctx context.Context, event webhook.Event) error {
	if event.Number == 0 || !e.served.serves(event.Owner, event.Repo) {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	seen, err := e.store.HasDelivery(ctx, event.ID)
	if err != nil || seen {
		return err
	}

	ended, err := e.close(ctx, event)
	if err != nil {
		return err
	}
	woken, err := e.wake(ctx, event)
	if err != nil {
		return err
	}
	if err := e.block(ctx, event, ended); err != nil {
		return err
	}
	for _, job := range ended {
		e.announce(job)
	}

	if !woken && len(ended) == 0 {
		return nil
	}
	return e.store.RecordDelivery(ctx, record.Delivery{
		ID:     event.ID,
		Kind:   event.Kind,
		Action: event.Action,
		Owner:  event.Owner,
		Repo:   event.Repo,
		Number: event.Number,
	})
}

// close stops a pull request's running jobs and ends the record's. A close
// writes the verdict the merge did (docs/adrs/001).
func (e *Engine) close(ctx context.Context, event webhook.Event) ([]record.Job, error) {
	if event.Kind != webhook.KindPullRequest || event.Action != "closed" {
		return nil, nil
	}
	if e.jobs != nil {
		e.jobs.Kill(event.Owner, event.Repo, event.Number)
	}
	return e.store.EndJobs(ctx, event.Owner, event.Repo, event.Number, event.Merged)
}

// wake starts every job the delivery wakes and reports whether a flow named
// one at all.
func (e *Engine) wake(ctx context.Context, event webhook.Event) (bool, error) {
	woken := false
	held := proved(event)
	for _, flow := range e.flows {
		for _, target := range e.targets(flow, held) {
			job, created, err := e.store.EnsureJob(ctx, record.Start{
				Flow:   flow.Name,
				Owner:  event.Owner,
				Repo:   event.Repo,
				Number: event.Number,
				Key:    flow.KeyFor(event.Number, target.agent),
				Agent:  target.agent,
				Title:  event.Title,
				Link:   event.Link,
				Task:   target.task,
			})
			if err != nil {
				return woken, err
			}
			if created {
				if e.jobs != nil {
					e.jobs.Start(*job)
				}
				e.announce(*job)
			}
			woken = true
		}
	}
	return woken, nil
}

// block writes the label an unmerged close leaves. A rejected write is kept
// on the jobs as their alert, so the record shows it instead of a silent pass.
func (e *Engine) block(ctx context.Context, event webhook.Event, ended []record.Job) error {
	if len(ended) == 0 || event.Merged {
		return nil
	}
	if err := e.labeler.Block(ctx, event.Owner, event.Repo, event.Number); err != nil {
		return e.store.SetAlert(ctx, event.Owner, event.Repo, event.Number, "blocked label: "+err.Error())
	}
	return nil
}

// target is one job a delivery starts: the task, and the agent a login named.
type target struct {
	agent string
	task  string
}

// targets returns the jobs a flow starts on this delivery: one for the first
// pull-request condition that holds, and one per login an agent holds for the
// first login condition that holds. A login no agent holds starts nothing.
func (e *Engine) targets(flow config.Flow, held proof) []target {
	for _, condition := range flow.Start {
		if !held.holds[condition.Key] {
			continue
		}
		if !config.PerLogin(condition.Key) {
			return []target{{task: condition.Target}}
		}
		var out []target
		seen := map[string]bool{}
		for _, login := range held.logins[condition.Key] {
			agent, ok := config.AgentFor(e.agents, login)
			if !ok || seen[agent] {
				continue
			}
			seen[agent] = true
			out = append(out, target{agent: agent, task: condition.Target})
		}
		return out
	}
	return nil
}

func (e *Engine) announce(job record.Job) {
	if e.publish != nil {
		e.publish(job.Flow, job.Owner, job.Repo, job.Key)
	}
}
