package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// PullRequest is the read a condition needs.
type PullRequest struct {
	Head   string
	Merged bool
}

// Review is one review submitted on a pull request.
type Review struct {
	State       string
	CommitID    string
	SubmittedAt time.Time
}

// PullRequest reads a pull request's head and merge state.
func (c *Client) PullRequest(ctx context.Context, owner, repo string, number int) (PullRequest, error) {
	var body struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
		Merged bool `json:"merged"`
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	if err := c.read(ctx, owner, repo, path, &body); err != nil {
		return PullRequest{}, err
	}
	return PullRequest{Head: body.Head.SHA, Merged: body.Merged}, nil
}

// Reviews reads the reviews submitted on a pull request.
func (c *Client) Reviews(ctx context.Context, owner, repo string, number int) ([]Review, error) {
	var body []struct {
		State       string    `json:"state"`
		CommitID    string    `json:"commit_id"`
		SubmittedAt time.Time `json:"submitted_at"`
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews?per_page=100",
		url.PathEscape(owner), url.PathEscape(repo), number)
	if err := c.read(ctx, owner, repo, path, &body); err != nil {
		return nil, err
	}
	reviews := make([]Review, 0, len(body))
	for _, review := range body {
		reviews = append(reviews, Review{State: review.State, CommitID: review.CommitID, SubmittedAt: review.SubmittedAt})
	}
	return reviews, nil
}

func (c *Client) read(ctx context.Context, owner, repo, path string, out any) error {
	token, err := c.token(ctx, owner, repo)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("github: %s: %w", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("github: %s: %s", path, response.Status)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("github: %s: %w", path, err)
	}
	return nil
}
