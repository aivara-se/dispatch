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

// Engine applies deliveries to the record.
type Engine struct {
	mu    sync.Mutex
	store *record.Store
	repos []string
	flows []config.Flow
}

// New returns an engine over one record, the served repositories and the
// configured flows.
func New(store *record.Store, repos []string, flows []config.Flow) *Engine {
	return &Engine{store: store, repos: repos, flows: flows}
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

	touched := false
	if event.Kind == webhook.KindPullRequest && event.Action == "closed" {
		ended, err := e.store.EndJobs(ctx, event.Owner, event.Repo, event.Number, event.Merged)
		if err != nil {
			return err
		}
		touched = ended > 0
	}

	holds := holding(event)
	for _, flow := range e.flows {
		if _, ok := startTarget(flow, holds); !ok {
			continue
		}
		key := strings.ReplaceAll(flow.Key, "{n}", strconv.Itoa(event.Number))
		if _, _, err := e.store.EnsureJob(ctx, flow.Name, event.Owner, event.Repo, event.Number, key); err != nil {
			return err
		}
		touched = true
	}

	if !touched {
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

// holding is the set of conditions this delivery proves. pr.updated is read
// after a task, never from a delivery, so it is never here.
func holding(event webhook.Event) map[string]bool {
	holds := map[string]bool{}
	switch event.Kind {
	case webhook.KindPullRequest:
		switch event.Action {
		case "opened":
			holds[config.ConditionCreated] = true
		case "closed":
			if event.Merged {
				holds[config.ConditionMerged] = true
			}
		}
	case webhook.KindPullRequestReview:
		if event.Action == "submitted" {
			switch event.Review {
			case "approved":
				holds[config.ConditionApproved] = true
			case "changes_requested":
				holds[config.ConditionRejected] = true
			}
		}
	}
	return holds
}

// startTarget returns the target of the first start condition that holds.
func startTarget(flow config.Flow, holds map[string]bool) (string, bool) {
	for _, condition := range flow.Start {
		if holds[condition.Key] {
			return condition.Target, true
		}
	}
	return "", false
}
