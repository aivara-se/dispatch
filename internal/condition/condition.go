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

// Holds reports whether key holds, with the read that decided it. pr.created
// is a delivery condition: after a task it never holds.
func (r *Reader) Holds(ctx context.Context, key, owner, repo string, number int, startHead string) (bool, json.RawMessage, error) {
	switch key {
	case "pr.created":
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
		state := lastOnHead(reviews, pull.Head)
		holds := state == "APPROVED"
		if key == "pr.rejected" {
			holds = state == "CHANGES_REQUESTED"
		}
		return holds, evidence(map[string]any{"head": pull.Head, "review": state}), nil
	default:
		return false, nil, fmt.Errorf("condition: unknown key %q", key)
	}
}

// lastOnHead returns the state of the last review submitted on head.
func lastOnHead(reviews []github.Review, head string) string {
	state := ""
	for _, review := range reviews {
		if review.CommitID == head {
			state = review.State
		}
	}
	return state
}

func evidence(value any) json.RawMessage {
	body, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return body
}
