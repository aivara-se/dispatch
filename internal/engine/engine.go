// Package engine turns a delivery into jobs: it reads each flow's start
// condition, starts the job it names, and ends jobs when the pull request
// closes. It runs no task in this version.
package engine

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/aivara-se/dispatch/internal/config"
	"github.com/aivara-se/dispatch/internal/record"
	"github.com/aivara-se/dispatch/internal/webhook"
)

// Labeler writes the blocked label for a pull request.
type Labeler interface {
	Block(ctx context.Context, owner, repo string, number int) error
}

// Publisher announces a job that changed.
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
	repos   []string
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
	return &Engine{store: store, repos: repos, flows: flows, agents: agents,
		labeler: labeler, publish: publish, jobs: jobs}
}

// Deliver applies one verified delivery. A delivery that is already kept, is
// outside the served repositories, or starts nothing changes nothing.
func (e *Engine) Deliver(ctx context.Context, event webhook.Event) error {
	if event.Number == 0 || !e.serves(event.Owner, event.Repo) {
		return nil
	}

	// The record is the only state, so one delivery is decided at a time and
	// a restart sees exactly what the last one wrote.
	e.mu.Lock()
	defer e.mu.Unlock()

	seen, err := e.store.HasDelivery(ctx, event.ID)
	if err != nil || seen {
		return err
	}

	var ended []record.Job
	touched := false
	if event.Kind == webhook.KindPullRequest && event.Action == "closed" {
		if e.jobs != nil {
			e.jobs.Kill(event.Owner, event.Repo, event.Number)
		}
		ended, err = e.store.EndJobs(ctx, event.Owner, event.Repo, event.Number, event.Merged)
		if err != nil {
			return err
		}
		touched = len(ended) > 0
	}

	proof := proved(event)
	for _, flow := range e.flows {
		for _, start := range e.starts(flow, proof) {
			key := strings.ReplaceAll(flow.Key, "{n}", strconv.Itoa(event.Number))
			key = strings.ReplaceAll(key, config.PlaceholderAgent, start.agent)
			job, created, err := e.store.EnsureJob(ctx, record.Start{
				Flow:   flow.Name,
				Owner:  event.Owner,
				Repo:   event.Repo,
				Number: event.Number,
				Key:    key,
				Agent:  start.agent,
				Title:  event.Title,
				Link:   event.Link,
				Task:   start.task,
			})
			if err != nil {
				return err
			}
			if created {
				if e.jobs != nil {
					e.jobs.Start(*job)
				}
				e.announce(*job)
			}
			touched = true
		}
	}

	// A close writes the label the flow names. A rejected write is kept on the
	// jobs as their alert, so the record shows it instead of a silent pass.
	if len(ended) > 0 && !event.Merged {
		if err := e.labeler.Block(ctx, event.Owner, event.Repo, event.Number); err != nil {
			if err := e.store.SetAlert(ctx, event.Owner, event.Repo, event.Number, "blocked label: "+err.Error()); err != nil {
				return err
			}
		}
	}

	// Announce after the write, so a client that refetches reads the alert.
	for _, job := range ended {
		e.announce(job)
	}

	if !touched {
		return nil
	}
	err = e.store.RecordDelivery(ctx, record.Delivery{
		ID:     event.ID,
		Kind:   event.Kind,
		Action: event.Action,
		Owner:  event.Owner,
		Repo:   event.Repo,
		Number: event.Number,
	})
	if err != nil {
		return err
	}
	return nil
}

func (e *Engine) announce(job record.Job) {
	if e.publish != nil {
		e.publish(job.Flow, job.Owner, job.Repo, job.Key)
	}
}

func (e *Engine) serves(owner, repo string) bool {
	for _, pattern := range e.repos {
		powner, pname, ok := strings.Cut(pattern, "/")
		if !ok || !strings.EqualFold(powner, owner) {
			continue
		}
		if pname == "*" || strings.EqualFold(pname, repo) {
			return true
		}
	}
	return false
}

// start is one job a delivery starts: the task, and the agent a login named.
type start struct {
	agent string
	task  string
}

// proof is what a delivery proves: the conditions that hold, and the logins
// each login condition names.
type proof struct {
	holds  map[string]bool
	logins map[string][]string
}

// proved is the set of conditions this delivery proves. pr.updated and
// pr.merged are read after a task, never from a delivery, so neither is here.
func proved(event webhook.Event) proof {
	p := proof{holds: map[string]bool{}, logins: map[string][]string{}}
	switch event.Kind {
	case webhook.KindPullRequest:
		switch event.Action {
		case "opened":
			p.holds[config.ConditionCreated] = true
			if len(event.Logins) > 0 {
				p.holds[config.ConditionReviewRequested] = true
				p.logins[config.ConditionReviewRequested] = event.Logins
			}
		case "review_requested":
			p.holds[config.ConditionReviewRequested] = true
			p.logins[config.ConditionReviewRequested] = event.Logins
		case "assigned":
			p.holds[config.ConditionAssigned] = true
			p.logins[config.ConditionAssigned] = event.Logins
		}
	case webhook.KindPullRequestReview:
		if event.Action == "submitted" {
			switch event.Review {
			case "approved":
				p.holds[config.ConditionApproved] = true
			case "changes_requested":
				p.holds[config.ConditionRejected] = true
			}
		}
	}
	return p
}

// starts returns the jobs a flow starts on this delivery: one for the first
// pull-request condition that holds, and one per login an agent holds for the
// first login condition that holds. A login no agent holds starts nothing.
func (e *Engine) starts(flow config.Flow, p proof) []start {
	for _, condition := range flow.Start {
		if !p.holds[condition.Key] {
			continue
		}
		if !config.PerLogin(condition.Key) {
			return []start{{task: condition.Target}}
		}
		var out []start
		seen := map[string]bool{}
		for _, login := range p.logins[condition.Key] {
			agent, ok := config.AgentFor(e.agents, login)
			if !ok || seen[agent] {
				continue
			}
			seen[agent] = true
			out = append(out, start{agent: agent, task: condition.Target})
		}
		return out
	}
	return nil
}
