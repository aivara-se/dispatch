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
	Login       string
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

// Reviews reads every review submitted on a pull request, oldest first.
func (c *Client) Reviews(ctx context.Context, owner, repo string, number int) ([]Review, error) {
	const perPage = 100
	var reviews []Review
	for page := 1; page <= 100; page++ {
		var body []struct {
			User struct {
				Login string `json:"login"`
			} `json:"user"`
			State       string    `json:"state"`
			CommitID    string    `json:"commit_id"`
			SubmittedAt time.Time `json:"submitted_at"`
		}
		path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews?per_page=%d&page=%d",
			url.PathEscape(owner), url.PathEscape(repo), number, perPage, page)
		if err := c.read(ctx, owner, repo, path, &body); err != nil {
			return nil, err
		}
		for _, review := range body {
			reviews = append(reviews, Review{
				Login:       review.User.Login,
				State:       review.State,
				CommitID:    review.CommitID,
				SubmittedAt: review.SubmittedAt,
			})
		}
		if len(body) < perPage {
			return reviews, nil
		}
	}
	return nil, fmt.Errorf("github: %s/%s#%d: reviews exceed 100 pages", owner, repo, number)
}

func (c *Client) read(ctx context.Context, owner, repo, path string, out any) error {
	ctx, cancel := c.bound(ctx)
	defer cancel()
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
