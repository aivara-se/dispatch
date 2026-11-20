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
	"sync"
	"time"
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

// Handler is the HTTP entry point. It verifies the signature before it parses
// anything and answers 202 as soon as the delivery is queued. One worker
// applies the queue in arrival order, outside the request, so a delivery that
// calls GitHub never holds the reply open.
type Handler struct {
	secret  []byte
	deliver func(context.Context, Event) error
	timeout time.Duration

	mu    sync.Mutex
	cond  *sync.Cond
	queue []Event
	busy  bool
}

// A delivery's apply may call GitHub; this bounds one apply.
const deliveryTimeout = 60 * time.Second

// A burst larger than this is refused with 503, and GitHub retries it.
const queueSize = 256

// NewHandler returns the webhook handler for one secret and one deliverer, and
// starts the worker that applies deliveries.
func NewHandler(secret []byte, deliver func(context.Context, Event) error) *Handler {
	h := &Handler{secret: secret, deliver: deliver, timeout: deliveryTimeout}
	h.cond = sync.NewCond(&h.mu)
	go h.work()
	return h
}

// Wait blocks until every queued delivery has been applied, or ctx ends.
func (h *Handler) Wait(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() {
		h.mu.Lock()
		h.cond.Broadcast()
		h.mu.Unlock()
	})
	defer stop()

	h.mu.Lock()
	defer h.mu.Unlock()
	for len(h.queue) > 0 || h.busy {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		h.cond.Wait()
	}
	return nil
}

// enqueue adds a delivery to the queue; a full queue reports false.
func (h *Handler) enqueue(event Event) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.queue) >= queueSize {
		return false
	}
	h.queue = append(h.queue, event)
	h.cond.Signal()
	return true
}

// work applies one delivery at a time, in arrival order.
func (h *Handler) work() {
	for {
		h.mu.Lock()
		for len(h.queue) == 0 {
			h.cond.Wait()
		}
		event := h.queue[0]
		h.queue = h.queue[1:]
		h.busy = true
		h.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
		err := h.deliver(ctx, event)
		cancel()
		if err != nil {
			log.Printf("dispatch: delivery %s: %v", event.ID, err)
		}

		h.mu.Lock()
		h.busy = false
		h.cond.Broadcast()
		h.mu.Unlock()
	}
}

// ServeHTTP verifies, parses and queues one request.
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
	if !h.enqueue(event) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
