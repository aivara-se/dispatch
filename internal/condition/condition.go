// Package condition reads the five task conditions from GitHub.
package condition

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aivara-se/dispatch/internal/github"
)

// Source is the pull request and review read the conditions need.
type Source interface {
	PullRequest(ctx context.Context, owner, repo string, number int) (github.PullRequest, error)
	Reviews(ctx context.Context, owner, repo string, number int) ([]github.Review, error)
}

// Reader reads a condition from one source.
type Reader struct {
	source Source
}

// New returns a reader over one GitHub client.
func New(source Source) *Reader {
	return &Reader{source: source}
}

// Head returns the pull request's current head.
func (r *Reader) Head(ctx context.Context, owner, repo string, number int) (string, error) {
	pull, err := r.source.PullRequest(ctx, owner, repo, number)
	if err != nil {
		return "", err
	}
	return pull.Head, nil
}

// Holds reports whether key holds, with the read that decided it. pr.created,
// pr.review_requested and pr.assigned are delivery conditions: after a task
// they never hold.
func (r *Reader) Holds(ctx context.Context, key, owner, repo string, number int, startHead string) (bool, json.RawMessage, error) {
	switch key {
	case "pr.created", "pr.review_requested", "pr.assigned":
		return false, evidence(map[string]any{"key": key, "delivery": true}), nil
	case "pr.merged":
		pull, err := r.source.PullRequest(ctx, owner, repo, number)
		if err != nil {
			return false, nil, err
		}
		return pull.Merged, evidence(map[string]any{"merged": pull.Merged}), nil
	case "pr.updated":
		pull, err := r.source.PullRequest(ctx, owner, repo, number)
		if err != nil {
			return false, nil, err
		}
		return pull.Head != startHead, evidence(map[string]any{"head": pull.Head, "start": startHead}), nil
	case "pr.approved", "pr.rejected":
		pull, err := r.source.PullRequest(ctx, owner, repo, number)
		if err != nil {
			return false, nil, err
		}
		reviews, err := r.source.Reviews(ctx, owner, repo, number)
		if err != nil {
			return false, nil, err
		}
		verdicts := verdictsOnHead(reviews, pull.Head)
		approved, rejected := false, false
		for _, state := range verdicts {
			approved = approved || state == "APPROVED"
			rejected = rejected || state == "CHANGES_REQUESTED"
		}
		holds := approved && !rejected
		if key == "pr.rejected" {
			holds = rejected
		}
		return holds, evidence(map[string]any{"head": pull.Head, "reviews": verdicts}), nil
	default:
		return false, nil, fmt.Errorf("condition: unknown key %q", key)
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
