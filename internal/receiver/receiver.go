// Package receiver verifies GitHub's deliveries and turns one into a run.
package receiver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aivara-se/dispatch/internal/audit"
)

// MaxBody is the largest delivery body the receiver will read.
const MaxBody = 1 << 20

// DedupTTL is how long a delivery id is remembered.
const DedupTTL = time.Hour

// MaxRounds is the number of reviews that request changes after which a pull
// request wakes nobody.
const MaxRounds = 10

// Runner starts a profile's run.
type Runner interface {
	Start(profile, reason string) error
}

// Recorder appends a delivery's audit line.
type Recorder interface {
	Append(audit.Record) error
}

// Receiver answers the one webhook route.
type Receiver struct {
	secret []byte
	agents map[string]string
	runner Runner
	audit  Recorder
	seen   *seen
	rounds *rounds
}

// New returns a Receiver for the given secret and configured login-to-profile
// map.
func New(secret []byte, agents map[string]string, runner Runner, recorder Recorder) *Receiver {
	return &Receiver{
		secret: secret,
		agents: agents,
		runner: runner,
		audit:  recorder,
		seen:   newSeen(DedupTTL, time.Now),
		rounds: newRounds(),
	}
}

type payload struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequest struct {
		Number int `json:"number"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
	RequestedReviewer *struct {
		Login string `json:"login"`
	} `json:"requested_reviewer"`
	Review struct {
		State string `json:"state"`
	} `json:"review"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

func (rc *Receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if err != nil {
		http.Error(w, "body too large", http.StatusBadRequest)
		return
	}
	if !rc.verify(body, r.Header.Get("X-Hub-Signature-256")) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	event := r.Header.Get("X-GitHub-Event")
	delivery := r.Header.Get("X-GitHub-Delivery")
	if event == "" || delivery == "" {
		http.Error(w, "missing event or delivery header", http.StatusBadRequest)
		return
	}

	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		http.Error(w, "body is not JSON", http.StatusBadRequest)
		return
	}

	rec := audit.Record{
		Delivery:   delivery,
		Event:      event,
		Repository: p.Repository.FullName,
		Number:     p.PullRequest.Number,
		Answer:     http.StatusAccepted,
	}
	rc.decide(&rec, &p)
	if err := rc.audit.Append(rec); err != nil {
		log.Printf("dispatch: audit: %v", err)
	}

	w.WriteHeader(http.StatusAccepted)
}

func (rc *Receiver) verify(body []byte, header string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got, err := hex.DecodeString(header[len(prefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, rc.secret)
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

func (rc *Receiver) decide(rec *audit.Record, p *payload) {
	review := rec.Event == "pull_request" && p.Action == "review_requested"
	changes := rec.Event == "pull_request_review" && p.Action == "submitted" && p.Review.State == "changes_requested"
	if !review && !changes {
		rec.Decision = audit.Ignored
		rec.Why = fmt.Sprintf("event %q with action %q is not a review request", rec.Event, p.Action)
		return
	}
	if rc.seen.has(rec.Delivery) {
		rec.Decision = audit.Duplicate
		rec.Why = "delivery already seen"
		return
	}
	rc.seen.add(rec.Delivery)

	if rc.rounds.reached(rec.Repository, rec.Number) {
		rec.Decision = audit.Ignored
		rec.Why = fmt.Sprintf("the %d-round cap was reached", MaxRounds)
		return
	}
	if changes {
		rc.rounds.add(rec.Repository, rec.Number)
	}

	login := p.PullRequest.User.Login
	reason := fmt.Sprintf("Dispatch: changes requested on %s#%d", rec.Repository, rec.Number)
	if review {
		if p.RequestedReviewer == nil || p.RequestedReviewer.Login == "" {
			rec.Decision = audit.Ignored
			rec.Why = "review was requested from a team, not a login"
			return
		}
		login = p.RequestedReviewer.Login
		reason = fmt.Sprintf("Dispatch: review requested on %s#%d", rec.Repository, rec.Number)
	}
	if p.Sender.Login == login {
		rec.Decision = audit.Ignored
		rec.Why = "the delivery's actor is the login to wake"
		return
	}
	profile, ok := rc.agents[login]
	if !ok {
		rec.Decision = audit.Ignored
		rec.Why = fmt.Sprintf("%q is not a configured agent", login)
		return
	}

	rec.Profile = profile
	rec.Reason = reason
	if err := rc.runner.Start(profile, reason); err != nil {
		rec.Decision = audit.StartFailed
		rec.Why = err.Error()
		return
	}
	rec.Decision = audit.Started
}

// rounds counts the reviews that requested changes, per pull request.
type rounds struct {
	mu    sync.Mutex
	count map[prKey]int
}

type prKey struct {
	repository string
	number     int
}

func newRounds() *rounds {
	return &rounds{count: make(map[prKey]int)}
}

// reached reports whether the pull request has had the cap's rounds.
func (r *rounds) reached(repository string, number int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count[prKey{repository, number}] >= MaxRounds
}

// add records one round for the pull request.
func (r *rounds) add(repository string, number int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := prKey{repository, number}
	if r.count[key] < MaxRounds {
		r.count[key]++
	}
}

// seen is the in-memory delivery-id cache.
type seen struct {
	mu  sync.Mutex
	ttl time.Duration
	now func() time.Time
	ids map[string]time.Time
}

func newSeen(ttl time.Duration, now func() time.Time) *seen {
	return &seen{ttl: ttl, now: now, ids: make(map[string]time.Time)}
}

func (s *seen) has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.ids[id]
	return ok && s.now().Sub(at) < s.ttl
}

func (s *seen) add(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, at := range s.ids {
		if now.Sub(at) >= s.ttl {
			delete(s.ids, key)
		}
	}
	s.ids[id] = now
}
