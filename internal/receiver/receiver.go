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

// Runner starts a profile's run and reports the child's exit status once.
type Runner interface {
	Start(profile, reason string, onExit func(exit int)) error
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
	pushRuns *counter
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
		pushRuns: newCounter(MaxFailures),
	}
}

type payload struct {
	Action     string `json:"action"`
	Repository struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
	PullRequest struct {
		Number    int             `json:"number"`
		State     string          `json:"state"`
		Draft     bool            `json:"draft"`
		MergedAt  json.RawMessage `json:"merged_at"`
		AutoMerge json.RawMessage `json:"auto_merge"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
		RequestedReviewers []struct {
			Login string `json:"login"`
		} `json:"requested_reviewers"`
	} `json:"pull_request"`
	RequestedReviewer *struct {
		Login string `json:"login"`
	} `json:"requested_reviewer"`
	Review struct {
		State             string `json:"state"`
		Body              string `json:"body"`
		AuthorAssociation string `json:"author_association"`
	} `json:"review"`
	Issue struct {
		Number int `json:"number"`
	} `json:"issue"`
	Assignee *struct {
		Login string `json:"login"`
	} `json:"assignee"`
	WorkflowRun struct {
		Conclusion string `json:"conclusion"`
		Event      string `json:"event"`
		HeadBranch string `json:"head_branch"`
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
	redMain := rec.Event == "workflow_run" && p.Action == "completed" && p.WorkflowRun.Conclusion == "failure" &&
		p.WorkflowRun.Event == "push" && len(p.WorkflowRun.PullRequests) == 0 &&
		p.WorkflowRun.HeadBranch != "" && p.WorkflowRun.HeadBranch == p.Repository.DefaultBranch
	mentioned := rec.Event == "issue_comment" && p.Action == "created" ||
		rec.Event == "pull_request_review" && p.Action == "submitted" ||
		rec.Event == "pull_request_review_comment" && p.Action == "created"
	approved := rec.Event == "pull_request_review" && p.Action == "submitted" && p.Review.State == "approved" &&
		p.PullRequest.State == "open" && !p.PullRequest.Draft && unarmed(p.PullRequest.AutoMerge)
	dismissed := rec.Event == "pull_request_review" && p.Action == "dismissed" &&
		p.PullRequest.State == "open" && !p.PullRequest.Draft && !merged(p.PullRequest.MergedAt)
	ready := rec.Event == "pull_request" && p.Action == "ready_for_review"
	if !review && !changes && !assigned && !failed && !redMain && !mentioned && !approved && !dismissed && !ready {
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

	capped := (review || changes) && rc.rounds.reached(pullRequestKey(rec.Repository, rec.Number))
	if capped && !mentioned {
		rec.Decision = audit.Ignored
		rec.Why = fmt.Sprintf("the %d-round cap was reached", MaxRounds)
		return
	}
	if failed && rc.failures.reached(pullRequestKey(rec.Repository, rec.Number)) {
		rec.Decision = audit.Ignored
		rec.Why = fmt.Sprintf("the %d-wake cap was reached", MaxFailures)
		return
	}
	if redMain && rc.pushRuns.reached(branchKey(rec.Repository, p.WorkflowRun.HeadBranch)) {
		rec.Decision = audit.Ignored
		rec.Why = fmt.Sprintf("the %d-wake cap was reached for the branch", MaxFailures)
		return
	}
	if changes && !capped {
		rc.rounds.add(pullRequestKey(rec.Repository, rec.Number))
	}
	if mentioned {
		rc.decideMention(rec, p, changes && !capped, approved, capped)
		return
	}
	if ready {
		logins := make([]string, 0, len(p.PullRequest.RequestedReviewers))
		for _, reviewer := range p.PullRequest.RequestedReviewers {
			logins = append(logins, reviewer.Login)
		}
		sort.Strings(logins)
		reason := fmt.Sprintf("Dispatch: ready for review on %s#%d", rec.Repository, rec.Number)
		if !rc.wakeAll(rec, logins, p.Sender.Login, reason) {
			rec.Decision = audit.Ignored
			rec.Why = "no configured reviewer to wake"
		}
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
	case failed:
		login = p.WorkflowRun.Actor.Login
		reason = fmt.Sprintf("Dispatch: checks failed on %s#%d", rec.Repository, rec.Number)
	case redMain:
		login = p.WorkflowRun.Actor.Login
		reason = fmt.Sprintf("Dispatch: checks failed on %s@%s", rec.Repository, p.WorkflowRun.HeadBranch)
	case dismissed:
		login = p.PullRequest.User.Login
		reason = fmt.Sprintf("Dispatch: approval dismissed on %s#%d", rec.Repository, rec.Number)
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
	if !failed && !redMain && p.Sender.Login == login {
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
	if err := rc.runner.Start(profile, reason, rc.exit(rec.Delivery, profile)); err != nil {
		rec.Decision = audit.StartFailed
		rec.Why = err.Error()
		return
	}
	rec.Decision = audit.Started
	if failed {
		rc.failures.add(pullRequestKey(rec.Repository, rec.Number))
	}
	if redMain {
		rc.pushRuns.add(branchKey(rec.Repository, p.WorkflowRun.HeadBranch))
	}
}

// exit returns the callback a started run reports its exit through: a non-zero
// status appends the run's own line, and a zero status appends nothing.
func (rc *Receiver) exit(delivery, profile string) func(int) {
	return func(code int) {
		if code == 0 {
			return
		}
		if err := rc.audit.Append(audit.Record{Delivery: delivery, Profile: profile, Exit: code}); err != nil {
			log.Printf("dispatch: audit: %v", err)
		}
	}
}

// unarmed reports whether a pull request's auto_merge is null.
func unarmed(autoMerge json.RawMessage) bool {
	return len(autoMerge) == 0 || string(autoMerge) == "null"
}

// merged reports whether a pull request's merged_at names a moment.
func merged(at json.RawMessage) bool {
	return len(at) > 0 && string(at) != "null"
}

// decideMention wakes each configured login a comment or a review names, and
// the author a review itself targets, on the delivery's one line. The author's
// reason leads the line; a mention's is its own. capped reports that the
// round cap refused the review's own targets: a mention consults neither cap.
func (rc *Receiver) decideMention(rec *audit.Record, p *payload, changes, approved, capped bool) {
	body, association := mentionSource(rec, p)

	var targets []target
	if changes {
		targets = append(targets, target{
			login:  p.PullRequest.User.Login,
			reason: fmt.Sprintf("Dispatch: changes requested on %s#%d", rec.Repository, rec.Number),
		})
	}
	if approved {
		targets = append(targets, target{
			login:  p.PullRequest.User.Login,
			reason: fmt.Sprintf("Dispatch: approved but not armed on %s#%d", rec.Repository, rec.Number),
		})
	}
	named := mentionedLogins(body, rc.agents)
	if mayMention(association) {
		reason := fmt.Sprintf("Dispatch: mentioned on %s#%d", rec.Repository, rec.Number)
		for _, login := range named {
			targets = append(targets, target{login: login, reason: reason})
		}
	}
	if len(targets) == 0 {
		rec.Decision = audit.Ignored
		switch {
		case len(named) > 0 && !mayMention(association):
			rec.Why = fmt.Sprintf("author association %q may not summon an agent", association)
		case capped:
			rec.Why = fmt.Sprintf("the %d-round cap was reached", MaxRounds)
		default:
			rec.Why = "no configured agent was mentioned"
		}
		return
	}

	if started, why := rc.wakeTargets(rec, targets, p.Sender.Login); !started {
		rec.Decision = audit.Ignored
		rec.Why = why
	}
}

// mentionSource returns the text a delivery's mention is read from and the
// association of its author.
func mentionSource(rec *audit.Record, p *payload) (body, association string) {
	if rec.Event == "pull_request_review" {
		return p.Review.Body, p.Review.AuthorAssociation
	}
	return p.Comment.Body, p.Comment.AuthorAssociation
}

// wakeAll starts one run per configured login, once, skipping the actor, and
// records the started profiles. It reports whether any run was started.
func (rc *Receiver) wakeAll(rec *audit.Record, logins []string, actor, reason string) bool {
	targets := make([]target, len(logins))
	for i, login := range logins {
		targets[i] = target{login: login, reason: reason}
	}
	started, _ := rc.wakeTargets(rec, targets, actor)
	return started
}

// target is one login to wake and the reason it is woken with.
type target struct {
	login  string
	reason string
}

// wakeTargets starts one run per target, once each and skipping the actor, and
// records the started profiles on the delivery's line with the first started
// target's reason. It reports whether any run was started, and when none was,
// why the first target was refused.
func (rc *Receiver) wakeTargets(rec *audit.Record, targets []target, actor string) (bool, string) {
	seen := make(map[string]bool, len(targets))
	profiles := make([]string, 0, len(targets))
	reasons := make([]string, 0, len(targets))
	why := ""
	for _, t := range targets {
		if t.login == "" {
			continue
		}
		if t.login == actor {
			if why == "" {
				why = "the delivery's actor is the login to wake"
			}
			continue
		}
		if seen[t.login] {
			continue
		}
		seen[t.login] = true
		profile, ok := rc.agents[t.login]
		if !ok {
			if why == "" {
				why = fmt.Sprintf("%q is not a configured agent", t.login)
			}
			continue
		}
		profiles = append(profiles, profile)
		reasons = append(reasons, t.reason)
	}
	if len(profiles) == 0 {
		return false, why
	}

	rec.Profile = strings.Join(profiles, ",")
	rec.Reason = reasons[0]
	var startErr error
	for i, profile := range profiles {
		if err := rc.runner.Start(profile, reasons[i], rc.exit(rec.Delivery, profile)); err != nil && startErr == nil {
			startErr = err
		}
	}
	if startErr != nil {
		rec.Decision = audit.StartFailed
		rec.Why = startErr.Error()
		return true, ""
	}
	rec.Decision = audit.Started
	return true, ""
}

// mayMention reports whether a comment's or review's author association may
// summon an agent.
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

// counter is an in-memory cap per key.
type counter struct {
	mu    sync.Mutex
	limit int
	count map[string]int
}

// pullRequestKey names one pull request's entry in a counter.
func pullRequestKey(repository string, number int) string {
	return fmt.Sprintf("%s#%d", repository, number)
}

// branchKey names one branch's entry in a counter.
func branchKey(repository, branch string) string {
	return repository + "@" + branch
}

func newCounter(limit int) *counter {
	return &counter{limit: limit, count: make(map[string]int)}
}

// reached reports whether the key's count has reached the counter's limit.
func (c *counter) reached(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count[key] >= c.limit
}

// add records one event for the key.
func (c *counter) add(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
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
