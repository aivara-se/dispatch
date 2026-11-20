// Package condition reads the closed condition set from GitHub. A condition
// is a question about a pull request, and every answer carries the read that
// decided it into the record.
package condition

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aivara-se/dispatch/internal/config"
	"github.com/aivara-se/dispatch/internal/github"
)

// Source is the pull request and review read every condition makes.
type Source interface {
	PullRequest(ctx context.Context, owner, repo string, number int) (github.PullRequest, error)
	Reviews(ctx context.Context, owner, repo string, number int) ([]github.Review, error)
}

// Reader evaluates one condition at a time against GitHub.
type Reader struct {
	source Source
}

// New returns a reader over one GitHub client.
func New(source Source) *Reader {
	return &Reader{source: source}
}

// Head returns the head the pull request carries now.
func (r *Reader) Head(ctx context.Context, owner, repo string, number int) (string, error) {
	pull, err := r.source.PullRequest(ctx, owner, repo, number)
	if err != nil {
		return "", err
	}
	return pull.Head, nil
}

// Holds reports whether key holds after a task, with the read that decided
// it. A key outside the closed set is an error, never false.
func (r *Reader) Holds(ctx context.Context, key, owner, repo string, number int, startHead string) (bool, json.RawMessage, error) {
	rule, ok := rules[key]
	if !ok {
		return false, nil, fmt.Errorf("condition: unknown key %q", key)
	}
	reading, err := r.observe(ctx, rule, owner, repo, number, startHead)
	if err != nil {
		return false, nil, err
	}
	return rule.holds(reading), evidence(rule.evidence(key, reading)), nil
}

func (r *Reader) observe(ctx context.Context, rule rule, owner, repo string, number int, startHead string) (reading, error) {
	out := reading{start: startHead}
	if !rule.pull {
		return out, nil
	}
	pull, err := r.source.PullRequest(ctx, owner, repo, number)
	if err != nil {
		return reading{}, err
	}
	out.pull = pull
	if !rule.reviews {
		return out, nil
	}
	reviews, err := r.source.Reviews(ctx, owner, repo, number)
	if err != nil {
		return reading{}, err
	}
	out.reviews = reviews
	return out, nil
}

// reading is what a rule evaluates: the reads it needs, and the head the task
// started on.
type reading struct {
	pull    github.PullRequest
	reviews []github.Review
	start   string
}

// rule is how one condition is read: which reads it costs, when it holds, and
// the evidence it records.
type rule struct {
	pull     bool
	reviews  bool
	holds    func(r reading) bool
	evidence func(key string, r reading) map[string]any
}

// rules is the closed condition set, keyed by the name a flow writes. Adding
// a condition is one entry here and one fact in config.
var rules = map[string]rule{
	config.ConditionCreated:         delivery(),
	config.ConditionReviewRequested: delivery(),
	config.ConditionAssigned:        delivery(),
	config.ConditionMerged: {
		pull:     true,
		holds:    func(r reading) bool { return r.pull.Merged },
		evidence: func(_ string, r reading) map[string]any { return map[string]any{"merged": r.pull.Merged} },
	},
	config.ConditionUpdated: {
		pull:     true,
		holds:    func(r reading) bool { return r.pull.Head != r.start },
		evidence: func(_ string, r reading) map[string]any { return map[string]any{"head": r.pull.Head, "start": r.start} },
	},
	config.ConditionApproved: verdict(true),
	config.ConditionRejected: verdict(false),
}

// delivery is a condition only a delivery proves, so no read holds it after a
// task.
func delivery() rule {
	return rule{
		holds:    func(r reading) bool { return false },
		evidence: func(key string, _ reading) map[string]any { return map[string]any{"key": key, "delivery": true} },
	}
}

// verdict reads each reviewer's latest verdict on the current head. An
// approval needs every verdict to be one, and holds against a change request;
// a rejection holds on any change request.
func verdict(approved bool) rule {
	return rule{
		pull:    true,
		reviews: true,
		holds: func(r reading) bool {
			verdicts := verdictsOnHead(r.reviews, r.pull.Head)
			yes, no := false, false
			for _, state := range verdicts {
				yes = yes || state == "APPROVED"
				no = no || state == "CHANGES_REQUESTED"
			}
			if approved {
				return yes && !no
			}
			return no
		},
		evidence: func(_ string, r reading) map[string]any {
			return map[string]any{"head": r.pull.Head, "reviews": verdictsOnHead(r.reviews, r.pull.Head)}
		},
	}
}

// verdictsOnHead returns each reviewer's latest verdict on head, by login. A
// comment or a pending review is not a verdict, so it leaves the last one
// standing.
func verdictsOnHead(reviews []github.Review, head string) map[string]string {
	verdicts := map[string]string{}
	for _, review := range reviews {
		if review.CommitID != head || review.State == "COMMENTED" || review.State == "PENDING" {
			continue
		}
		verdicts[review.Login] = review.State
	}
	return verdicts
}

func evidence(value any) json.RawMessage {
	body, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return body
}
