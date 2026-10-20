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
	"sort"
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

// MaxFailures is the number of failed-check wakes a pull request gets.
const MaxFailures = 5

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
	secret   []byte
	agents   map[string]string
	runner   Runner
	audit    Recorder
	seen     *seen
	rounds   *counter
	failures *counter
}

// New returns a Receiver for the given secret and configured login-to-profile
// map.
func New(secret []byte, agents map[string]string, runner Runner, recorder Recorder) *Receiver {
	return &Receiver{
		secret:   secret,
		agents:   agents,
		runner:   runner,
		audit:    recorder,
		seen:     newSeen(DedupTTL, time.Now),
		rounds:   newCounter(MaxRounds),
		failures: newCounter(MaxFailures),
	}
}

type payload struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequest struct {
		Number    int             `json:"number"`
		State     string          `json:"state"`
		Draft     bool            `json:"draft"`
		AutoMerge json.RawMessage `json:"auto_merge"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
	RequestedReviewer *struct {
		Login string `json:"login"`
	} `json:"requested_reviewer"`
	Review struct {
		State string `json:"state"`
	} `json:"review"`
	Issue struct {
		Number int `json:"number"`
	} `json:"issue"`
	Assignee *struct {
		Login string `json:"login"`
	} `json:"assignee"`
	WorkflowRun struct {
		Conclusion string `json:"conclusion"`
		Actor      struct {
			Login string `json:"login"`
		} `json:"actor"`
		PullRequests []struct {
			Number int `json:"number"`
		} `json:"pull_requests"`
	} `json:"workflow_run"`
	Comment struct {
		Body              string `json:"body"`
		AuthorAssociation string `json:"author_association"`
	} `json:"comment"`
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
	switch event {
	case "issues", "issue_comment":
		rec.Number = p.Issue.Number
	case "workflow_run":
		if len(p.WorkflowRun.PullRequests) > 0 {
			rec.Number = p.WorkflowRun.PullRequests[0].Number
		}
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
	assigned := (rec.Event == "issues" || rec.Event == "pull_request") && p.Action == "assigned"
	failed := rec.Event == "workflow_run" && p.Action == "completed" && p.WorkflowRun.Conclusion == "failure" && len(p.WorkflowRun.PullRequests) > 0
	mentioned := rec.Event == "issue_comment" && p.Action == "created"
	approved := rec.Event == "pull_request_review" && p.Action == "submitted" && p.Review.State == "approved" &&
		p.PullRequest.State == "open" && !p.PullRequest.Draft && unarmed(p.PullRequest.AutoMerge)
	if !review && !changes && !assigned && !failed && !mentioned && !approved {
		rec.Decision = audit.Ignored
		rec.Why = fmt.Sprintf("event %q with action %q is not a delivery that acts", rec.Event, p.Action)
		return
	}
	if rc.seen.has(rec.Delivery) {
		rec.Decision = audit.Duplicate
		rec.Why = "delivery already seen"
		return
	}
	rc.seen.add(rec.Delivery)

	if (review || changes) && rc.rounds.reached(rec.Repository, rec.Number) {
		rec.Decision = audit.Ignored
		rec.Why = fmt.Sprintf("the %d-round cap was reached", MaxRounds)
		return
	}
	if failed && rc.failures.reached(rec.Repository, rec.Number) {
		rec.Decision = audit.Ignored
		rec.Why = fmt.Sprintf("the %d-wake cap was reached", MaxFailures)
		return
	}
	if changes {
		rc.rounds.add(rec.Repository, rec.Number)
	}
	if mentioned {
		rc.decideMention(rec, p)
		return
	}

	var login, reason string
	switch {
	case review:
		if p.RequestedReviewer == nil || p.RequestedReviewer.Login == "" {
			rec.Decision = audit.Ignored
			rec.Why = "review was requested from a team, not a login"
			return
		}
		login = p.RequestedReviewer.Login
		reason = fmt.Sprintf("Dispatch: review requested on %s#%d", rec.Repository, rec.Number)
	case changes:
		login = p.PullRequest.User.Login
		reason = fmt.Sprintf("Dispatch: changes requested on %s#%d", rec.Repository, rec.Number)
	case failed:
		login = p.WorkflowRun.Actor.Login
		reason = fmt.Sprintf("Dispatch: checks failed on %s#%d", rec.Repository, rec.Number)
	case approved:
		login = p.PullRequest.User.Login
		reason = fmt.Sprintf("Dispatch: approved but not armed on %s#%d", rec.Repository, rec.Number)
	default:
		if p.Assignee == nil || p.Assignee.Login == "" {
			rec.Decision = audit.Ignored
			rec.Why = "the delivery's assignee is missing"
			return
		}
		login = p.Assignee.Login
		reason = fmt.Sprintf("Dispatch: issue assigned on %s#%d", rec.Repository, rec.Number)
		if rec.Event == "pull_request" {
			reason = fmt.Sprintf("Dispatch: pull request assigned on %s#%d", rec.Repository, rec.Number)
		}
	}
	if !failed && p.Sender.Login == login {
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
	if failed {
		rc.failures.add(rec.Repository, rec.Number)
	}
}

// unarmed reports whether a pull request's auto_merge is null.
func unarmed(autoMerge json.RawMessage) bool {
	return len(autoMerge) == 0 || string(autoMerge) == "null"
}

// decideMention wakes each configured agent named in a comment, once.
func (rc *Receiver) decideMention(rec *audit.Record, p *payload) {
	if !mayMention(p.Comment.AuthorAssociation) {
		rec.Decision = audit.Ignored
		rec.Why = fmt.Sprintf("author association %q may not summon an agent", p.Comment.AuthorAssociation)
		return
	}
	logins := mentionedLogins(p.Comment.Body, rc.agents)
	if len(logins) == 0 {
		rec.Decision = audit.Ignored
		rec.Why = "no configured agent was mentioned"
		return
	}

	profiles := make([]string, 0, len(logins))
	for _, login := range logins {
		if login != p.Sender.Login {
			profiles = append(profiles, rc.agents[login])
		}
	}
	if len(profiles) == 0 {
		rec.Decision = audit.Ignored
		rec.Why = "the delivery's actor is the login to wake"
		return
	}

	reason := fmt.Sprintf("Dispatch: mentioned on %s#%d", rec.Repository, rec.Number)
	rec.Profile = strings.Join(profiles, ",")
	rec.Reason = reason
	var startErr error
	for _, profile := range profiles {
		if err := rc.runner.Start(profile, reason); err != nil && startErr == nil {
			startErr = err
		}
	}
	if startErr != nil {
		rec.Decision = audit.StartFailed
		rec.Why = startErr.Error()
		return
	}
	rec.Decision = audit.Started
}

// mayMention reports whether a comment's author association may summon an
// agent.
func mayMention(association string) bool {
	switch association {
	case "OWNER", "MEMBER", "COLLABORATOR":
		return true
	}
	return false
}

// mentionedLogins returns the configured logins named exactly in body, once
// each and sorted.
func mentionedLogins(body string, agents map[string]string) []string {
	var logins []string
	for login := range agents {
		if mentions(body, login) {
			logins = append(logins, login)
		}
	}
	sort.Strings(logins)
	return logins
}

// mentions reports whether body names login as @login, bounded so a longer
// login or a preceding login character does not match.
func mentions(body, login string) bool {
	for i := 0; i+1+len(login) <= len(body); i++ {
		if body[i] != '@' {
			continue
		}
		if i > 0 && isLoginByte(body[i-1]) {
			continue
		}
		if !strings.EqualFold(body[i+1:i+1+len(login)], login) {
			continue
		}
		end := i + 1 + len(login)
		if end < len(body) && isLoginByte(body[end]) {
			continue
		}
		return true
	}
	return false
}

// isLoginByte reports whether b can be part of a GitHub login.
func isLoginByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-'
}

// counter is an in-memory cap per pull request.
type counter struct {
	mu    sync.Mutex
	limit int
	count map[prKey]int
}

type prKey struct {
	repository string
	number     int
}

func newCounter(limit int) *counter {
	return &counter{limit: limit, count: make(map[prKey]int)}
}

// reached reports whether the pull request has reached the counter's limit.
func (c *counter) reached(repository string, number int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count[prKey{repository, number}] >= c.limit
}

// add records one event for the pull request.
func (c *counter) add(repository string, number int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := prKey{repository, number}
	if c.count[key] < c.limit {
		c.count[key]++
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
