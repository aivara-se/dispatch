package github

import (
	"context"
	"fmt"
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
	if err := c.read(ctx, owner, repo, http.MethodGet, path, nil, &body); err != nil {
		return PullRequest{}, err
	}
	return PullRequest{Head: body.Head.SHA, Merged: body.Merged}, nil
}

// Reviews reads every review submitted on a pull request, oldest first. A
// pull request with more than ten thousand reviews is refused, so no read
// runs without a bound.
func (c *Client) Reviews(ctx context.Context, owner, repo string, number int) ([]Review, error) {
	const perPage = 100
	const maxPages = 100
	var reviews []Review
	for page := 1; page <= maxPages; page++ {
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
		if err := c.read(ctx, owner, repo, http.MethodGet, path, nil, &body); err != nil {
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
	return nil, fmt.Errorf("github: %s/%s#%d: reviews exceed %d pages", owner, repo, number, maxPages)
}
