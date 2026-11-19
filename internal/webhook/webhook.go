// Package webhook verifies GitHub deliveries, reads the pull request they
// name, and hands the event to the engine.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
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

// The largest body GitHub posts is well under this.
const maxBody = 32 << 20

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
			Number  int    `json:"number"`
			Merged  bool   `json:"merged"`
			Title   string `json:"title"`
			HTMLURL string `json:"html_url"`
		} `json:"pull_request"`
		Review *struct {
			State string `json:"state"`
		} `json:"review"`
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
	return event, nil
}

// Handler is the HTTP entry point. It verifies the signature before it parses
// anything, and answers 202 for a delivery that starts nothing.
type Handler struct {
	secret  []byte
	deliver func(context.Context, Event) error
}

// NewHandler returns the webhook handler for one secret and one deliverer.
func NewHandler(secret []byte, deliver func(context.Context, Event) error) *Handler {
	return &Handler{secret: secret, deliver: deliver}
}

// ServeHTTP verifies, parses and delivers one request.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !Verify(h.secret, body, r.Header.Get("X-Hub-Signature-256")) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	event, err := Parse(r.Header, body)
	switch {
	case errors.Is(err, ErrNoPullRequest):
		w.WriteHeader(http.StatusAccepted)
		return
	case errors.Is(err, ErrNoDelivery):
		w.WriteHeader(http.StatusBadRequest)
		return
	case err != nil:
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err := h.deliver(r.Context(), event); err != nil {
		log.Printf("dispatch: delivery %s: %v", event.ID, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
