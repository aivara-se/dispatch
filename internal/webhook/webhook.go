// Package webhook verifies GitHub deliveries, reads the pull request they
// name, and queues the event for the engine.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// The two GitHub events the engine subscribes to.
const (
	KindPullRequest       = "pull_request"
	KindPullRequestReview = "pull_request_review"
)

// ErrNoPullRequest reports a delivery that names no pull request.
var ErrNoPullRequest = errors.New("delivery names no pull request")

// ErrNoDelivery reports a delivery without an id.
var ErrNoDelivery = errors.New("delivery has no id")

// Event is a delivery reduced to what the engine decides on.
type Event struct {
	ID     string
	Kind   string
	Action string
	Owner  string
	Repo   string
	Number int
	Merged bool
	Review string
	Title  string
	Link   string
	// Logins are the users a review request or an assignment names, in the
	// payload's order.
	Logins []string
}

// Verify reports whether signature is the HMAC-SHA256 of body under secret.
func Verify(secret, body []byte, signature string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(signature, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(want, mac.Sum(nil))
}

// Parse reads the event headers and the payload. It returns ErrNoPullRequest
// when the event is not one of the two, or names no pull request, and
// ErrNoDelivery when the delivery has no id.
func Parse(header http.Header, body []byte) (Event, error) {
	id := header.Get("X-GitHub-Delivery")
	if id == "" {
		return Event{}, ErrNoDelivery
	}
	kind := header.Get("X-GitHub-Event")
	if kind != KindPullRequest && kind != KindPullRequestReview {
		return Event{}, ErrNoPullRequest
	}
	var payload struct {
		Action     string `json:"action"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest *struct {
			Number             int    `json:"number"`
			Merged             bool   `json:"merged"`
			Title              string `json:"title"`
			HTMLURL            string `json:"html_url"`
			RequestedReviewers []struct {
				Login string `json:"login"`
			} `json:"requested_reviewers"`
		} `json:"pull_request"`
		Review *struct {
			State string `json:"state"`
		} `json:"review"`
		RequestedReviewer *struct {
			Login string `json:"login"`
		} `json:"requested_reviewer"`
		Assignee *struct {
			Login string `json:"login"`
		} `json:"assignee"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Event{}, err
	}
	if payload.PullRequest == nil || payload.PullRequest.Number == 0 {
		return Event{}, ErrNoPullRequest
	}
	owner, repo, ok := strings.Cut(payload.Repository.FullName, "/")
	if !ok || owner == "" || repo == "" {
		return Event{}, ErrNoPullRequest
	}
	event := Event{
		ID:     id,
		Kind:   kind,
		Action: payload.Action,
		Owner:  owner,
		Repo:   repo,
		Number: payload.PullRequest.Number,
		Merged: payload.PullRequest.Merged,
		Title:  payload.PullRequest.Title,
		Link:   payload.PullRequest.HTMLURL,
	}
	if payload.Review != nil {
		event.Review = payload.Review.State
	}
	switch payload.Action {
	case "review_requested":
		if payload.RequestedReviewer != nil && payload.RequestedReviewer.Login != "" {
			event.Logins = []string{payload.RequestedReviewer.Login}
		}
	case "assigned":
		if payload.Assignee != nil && payload.Assignee.Login != "" {
			event.Logins = []string{payload.Assignee.Login}
		}
	case "opened":
		for _, reviewer := range payload.PullRequest.RequestedReviewers {
			if reviewer.Login != "" {
				event.Logins = append(event.Logins, reviewer.Login)
			}
		}
	}
	return event, nil
}
