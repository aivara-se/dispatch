package engine_test

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aivara-se/dispatch/internal/config"
	"github.com/aivara-se/dispatch/internal/engine"
	"github.com/aivara-se/dispatch/internal/github"
	"github.com/aivara-se/dispatch/internal/record"
	"github.com/aivara-se/dispatch/internal/webhook"
)

const dispatchFile = `{
  "listen": "127.0.0.1:8645",
  "path": "/github",
  "secret_name": "DISPATCH_WEBHOOK_SECRET",
  "app_id": 123456,
  "key_path": "/tmp/app.pem",
  "repos": ["aivara-se/*"],
  "hermes": "/usr/local/bin/hermes",
  "agents": {
    "memo": { "profile": "memo", "login": "memo-bot" },
    "momo": { "profile": "momo", "login": "momo-bot" }
  }
}`

const reviewFlow = `{
  "key": "{n}",
  "tasks": {
    "_start_": { "next": { "pr.created": "review" } },
    "review": {
      "actions": [],
      "limit": { "turn": 1, "time": 60 },
      "next": { "pr.approved": "_done_" }
    }
  }
}`

const approveFlow = `{
  "key": "approve-{n}",
  "tasks": {
    "_start_": { "next": { "pr.approved": "note" } },
    "note": {
      "actions": [],
      "limit": { "turn": 1, "time": 60 },
      "next": { "pr.rejected": "_done_" }
    }
  }
}`

const auditFlow = `{
  "key": "audit-{n}",
  "tasks": {
    "_start_": { "next": { "pr.created": "audit" } },
    "audit": {
      "actions": [],
      "limit": { "turn": 1, "time": 60 },
      "next": { "pr.merged": "_done_" }
    }
  }
}`

const reviewerFlow = `{
  "key": "{n}-{agent}",
  "tasks": {
    "_start_": { "next": { "pr.review_requested": "review" } },
    "review": {
      "actions": [ { "type": "prompt", "agent": "{agent}", "text": "Review {pr.url}." } ],
      "limit": { "turn": 1, "time": 60 },
      "next": { "pr.approved": "_done_" }
    }
  }
}`

const assignFlow = `{
  "key": "{n}-{agent}",
  "tasks": {
    "_start_": { "next": { "pr.assigned": "work" } },
    "work": {
      "actions": [ { "type": "prompt", "agent": "{agent}", "text": "Own {pr.url}." } ],
      "limit": { "turn": 1, "time": 60 },
      "next": { "pr.merged": "_done_" }
    }
  }
}`

var secret = []byte("s3cret")

type harness struct {
	t       *testing.T
	store   *record.Store
	handler *webhook.Handler
	server  *httptest.Server
	dbPath  string
	labeler engine.Labeler
	stub    *stubLabeler
	events  []string
	alerts  []string
	started []string
	killed  []string
}

func (h *harness) Start(job record.Job) {
	h.started = append(h.started, job.Key)
}

func (h *harness) Kill(owner, repo string, number int) {
	h.killed = append(h.killed, owner+"/"+repo+"#"+strconv.Itoa(number))
}

type stubLabeler struct {
	calls  int
	err    error
	owner  string
	repo   string
	number int
}

func (s *stubLabeler) Block(_ context.Context, owner, repo string, number int) error {
	s.calls++
	s.owner, s.repo, s.number = owner, repo, number
	return s.err
}

func newHarness(t *testing.T, flows map[string]string) *harness {
	t.Helper()
	stub := &stubLabeler{}
	h := newHarnessWith(t, flows, stub)
	h.stub = stub
	return h
}

func newHarnessWith(t *testing.T, flows map[string]string, labeler engine.Labeler) *harness {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "flows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dispatch.json"), []byte(dispatchFile), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, body := range flows {
		if err := os.WriteFile(filepath.Join(dir, "flows", name+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg, loaded, err := config.Load(dir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	dbPath := filepath.Join(dir, "dispatch.db")
	store, err := record.Open(dbPath)
	if err != nil {
		t.Fatalf("open record: %v", err)
	}
	mux := http.NewServeMux()
	h := &harness{t: t, store: store, dbPath: dbPath, labeler: labeler}
	h.handler = webhook.NewHandler(secret, engine.New(store, cfg.Repos, loaded, cfg.Agents, h.labeler, h.publish, h).Deliver)
	mux.Handle(cfg.Path, h.handler)
	server := httptest.NewServer(mux)
	h.server = server
	t.Cleanup(func() {
		server.Close()
		store.Close()
	})
	return h
}

func (h *harness) publish(flow, owner, repo, key string) {
	h.events = append(h.events, flow+"/"+owner+"/"+repo+"/"+key)
	job, err := h.store.JobByKey(context.Background(), flow, owner, repo, key)
	if err == nil {
		h.alerts = append(h.alerts, job.Alert)
	}
}

type delivery struct {
	id           string
	event        string
	action       string
	owner        string
	repo         string
	number       int
	merged       bool
	review       string
	logins       []string
	badSignature bool
}

func (h *harness) post(d delivery) int {
	h.t.Helper()
	if d.event == "" {
		d.event = webhook.KindPullRequest
	}
	if d.id == "" {
		d.id = "d1"
	}
	if d.owner == "" {
		d.owner = "aivara-se"
	}
	if d.repo == "" {
		d.repo = "dispatch"
	}
	payload := struct {
		Action     string `json:"action"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest struct {
			Number             int    `json:"number"`
			Merged             bool   `json:"merged"`
			Title              string `json:"title"`
			HTMLURL            string `json:"html_url"`
			RequestedReviewers []struct {
				Login string `json:"login"`
			} `json:"requested_reviewers,omitempty"`
		} `json:"pull_request"`
		Review *struct {
			State string `json:"state"`
		} `json:"review,omitempty"`
		RequestedReviewer *struct {
			Login string `json:"login"`
		} `json:"requested_reviewer,omitempty"`
		Assignee *struct {
			Login string `json:"login"`
		} `json:"assignee,omitempty"`
	}{Action: d.action}
	payload.Repository.FullName = d.owner + "/" + d.repo
	payload.PullRequest.Number = d.number
	payload.PullRequest.Merged = d.merged
	payload.PullRequest.Title = "a title"
	payload.PullRequest.HTMLURL = "https://github.com/" + d.owner + "/" + d.repo + "/pull/" + strconv.Itoa(d.number)
	switch {
	case d.action == "review_requested" && len(d.logins) > 0:
		payload.RequestedReviewer = &struct {
			Login string `json:"login"`
		}{Login: d.logins[0]}
	case d.action == "assigned" && len(d.logins) > 0:
		payload.Assignee = &struct {
			Login string `json:"login"`
		}{Login: d.logins[0]}
	case d.action == "opened":
		for _, login := range d.logins {
			payload.PullRequest.RequestedReviewers = append(payload.PullRequest.RequestedReviewers,
				struct {
					Login string `json:"login"`
				}{Login: login})
		}
	}
	if d.review != "" {
		payload.Review = &struct {
			State string `json:"state"`
		}{State: d.review}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		h.t.Fatal(err)
	}

	key := secret
	if d.badSignature {
		key = []byte("another secret")
	}
	request, err := http.NewRequest(http.MethodPost, h.server.URL+"/github", strings.NewReader(string(body)))
	if err != nil {
		h.t.Fatal(err)
	}
	request.Header.Set("X-GitHub-Event", d.event)
	request.Header.Set("X-GitHub-Delivery", d.id)
	request.Header.Set("X-Hub-Signature-256", sign(key, body))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		h.t.Fatal(err)
	}
	response.Body.Close()
	// The handler answers 202 before it applies the delivery, so the test waits
	// for the queue the way a restart would.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.handler.Wait(ctx); err != nil {
		h.t.Fatalf("wait for delivery %s: %v", d.id, err)
	}
	return response.StatusCode
}

func (h *harness) count(table string) int {
	h.t.Helper()
	db, err := sql.Open("sqlite", "file:"+h.dbPath)
	if err != nil {
		h.t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		h.t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func (h *harness) job(flow string, agent ...string) *record.Job {
	h.t.Helper()
	name := ""
	if len(agent) > 0 {
		name = agent[0]
	}
	job, err := h.store.Job(context.Background(), flow, "aivara-se", "dispatch", 212, name)
	if err != nil {
		h.t.Fatalf("job %s: %v", flow, err)
	}
	return job
}

func TestOpenedDeliveryStartsOneJob(t *testing.T) {
	h := newHarness(t, map[string]string{"review": reviewFlow})
	if status := h.post(delivery{action: "opened", number: 212}); status != http.StatusAccepted {
		t.Fatalf("status = %d", status)
	}
	job := h.job("review")
	if job.State != record.StateStarted || job.Verdict != record.VerdictPending || job.Key != "212" {
		t.Fatalf("job = %+v", job)
	}
	if job.Task != "review" {
		t.Fatalf("task = %q", job.Task)
	}
	if len(h.started) != 1 || h.started[0] != "212" {
		t.Fatalf("started = %v", h.started)
	}
	if job.Title != "a title" || job.Link != "https://github.com/aivara-se/dispatch/pull/212" {
		t.Fatalf("job title = %q, link = %q", job.Title, job.Link)
	}
	if jobs, deliveries := h.count("jobs"), h.count("deliveries"); jobs != 1 || deliveries != 1 {
		t.Fatalf("jobs = %d, deliveries = %d", jobs, deliveries)
	}
	if len(h.events) != 1 || h.events[0] != "review/aivara-se/dispatch/212" {
		t.Fatalf("events = %v", h.events)
	}
}

func TestDuplicateDeliveryStartsNoSecondJob(t *testing.T) {
	h := newHarness(t, map[string]string{"review": reviewFlow})
	for i := 0; i < 2; i++ {
		if status := h.post(delivery{action: "opened", number: 212}); status != http.StatusAccepted {
			t.Fatalf("post %d status = %d", i, status)
		}
	}
	if jobs, deliveries := h.count("jobs"), h.count("deliveries"); jobs != 1 || deliveries != 1 {
		t.Fatalf("jobs = %d, deliveries = %d", jobs, deliveries)
	}
}

func TestSecondDeliveryStartsNoSecondJob(t *testing.T) {
	h := newHarness(t, map[string]string{"review": reviewFlow})
	if status := h.post(delivery{id: "d1", action: "opened", number: 212}); status != http.StatusAccepted {
		t.Fatalf("first status = %d", status)
	}
	if status := h.post(delivery{id: "d2", action: "opened", number: 212}); status != http.StatusAccepted {
		t.Fatalf("second status = %d", status)
	}
	if jobs, deliveries := h.count("jobs"), h.count("deliveries"); jobs != 1 || deliveries != 2 {
		t.Fatalf("jobs = %d, deliveries = %d", jobs, deliveries)
	}
}

func TestBadSignatureWritesNothing(t *testing.T) {
	h := newHarness(t, map[string]string{"review": reviewFlow})
	if status := h.post(delivery{action: "opened", number: 212, badSignature: true}); status != http.StatusUnauthorized {
		t.Fatalf("status = %d", status)
	}
	if jobs, deliveries := h.count("jobs"), h.count("deliveries"); jobs != 0 || deliveries != 0 {
		t.Fatalf("jobs = %d, deliveries = %d", jobs, deliveries)
	}
}

func TestUnservedRepositoryStartsNothing(t *testing.T) {
	h := newHarness(t, map[string]string{"review": reviewFlow})
	if status := h.post(delivery{action: "opened", number: 212, owner: "other", repo: "repo"}); status != http.StatusAccepted {
		t.Fatalf("status = %d", status)
	}
	if jobs, deliveries := h.count("jobs"), h.count("deliveries"); jobs != 0 || deliveries != 0 {
		t.Fatalf("jobs = %d, deliveries = %d", jobs, deliveries)
	}
}

func TestEventThatStartsNoFlowWritesNothing(t *testing.T) {
	h := newHarness(t, map[string]string{"review": reviewFlow})
	if status := h.post(delivery{action: "edited", number: 212}); status != http.StatusAccepted {
		t.Fatalf("status = %d", status)
	}
	if jobs, deliveries := h.count("jobs"), h.count("deliveries"); jobs != 0 || deliveries != 0 {
		t.Fatalf("jobs = %d, deliveries = %d", jobs, deliveries)
	}
}

func TestReviewStartsOnlyTheFlowThatNamesIt(t *testing.T) {
	h := newHarness(t, map[string]string{"review": reviewFlow, "approve": approveFlow})
	if status := h.post(delivery{
		id:     "review-1",
		event:  webhook.KindPullRequestReview,
		action: "submitted",
		review: "approved",
		number: 212,
	}); status != http.StatusAccepted {
		t.Fatalf("status = %d", status)
	}
	job := h.job("approve")
	if job.State != record.StateStarted || job.Key != "approve-212" {
		t.Fatalf("approve job = %+v", job)
	}
	if _, err := h.store.Job(context.Background(), "review", "aivara-se", "dispatch", 212, ""); !errors.Is(err, record.ErrNotFound) {
		t.Fatalf("review job error = %v", err)
	}
}

func TestClosedPullRequestEndsEveryJob(t *testing.T) {
	cases := []struct {
		name    string
		merged  bool
		state   string
		verdict string
	}{
		{name: "unmerged", merged: false, state: record.StateBlocked, verdict: record.VerdictFailed},
		{name: "merged", merged: true, state: record.StateDone, verdict: record.VerdictPassed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, map[string]string{"review": reviewFlow, "audit": auditFlow})
			if status := h.post(delivery{id: "open-1", action: "opened", number: 212}); status != http.StatusAccepted {
				t.Fatalf("open status = %d", status)
			}
			if jobs := h.count("jobs"); jobs != 2 {
				t.Fatalf("jobs before close = %d", jobs)
			}
			if status := h.post(delivery{id: "close-1", action: "closed", number: 212, merged: tc.merged}); status != http.StatusAccepted {
				t.Fatalf("close status = %d", status)
			}
			for _, flow := range []string{"review", "audit"} {
				job := h.job(flow)
				if job.State != tc.state || job.Verdict != tc.verdict {
					t.Fatalf("%s job = %s/%s", flow, job.State, job.Verdict)
				}
			}
			if len(h.killed) != 1 || h.killed[0] != "aivara-se/dispatch#212" {
				t.Fatalf("killed = %v", h.killed)
			}
			wantCalls := 1
			if tc.merged {
				wantCalls = 0
			}
			if h.stub.calls != wantCalls {
				t.Fatalf("label calls = %d, want %d", h.stub.calls, wantCalls)
			}
			if tc.merged {
				if h.stub.owner != "" {
					t.Fatalf("a merged close called the labeler for %s", h.stub.owner)
				}
			} else if h.stub.owner != "aivara-se" || h.stub.repo != "dispatch" || h.stub.number != 212 {
				t.Fatalf("labeler call = %s/%s#%d", h.stub.owner, h.stub.repo, h.stub.number)
			}
		})
	}
}

func TestRejectedLabelIsRecorded(t *testing.T) {
	h := newHarness(t, map[string]string{"review": reviewFlow})
	h.stub.err = errors.New("github: label aivara-se/dispatch#212: 403 Forbidden")
	if status := h.post(delivery{id: "open-1", action: "opened", number: 212}); status != http.StatusAccepted {
		t.Fatalf("open status = %d", status)
	}
	if status := h.post(delivery{id: "close-1", action: "closed", number: 212}); status != http.StatusAccepted {
		t.Fatalf("close status = %d", status)
	}
	job := h.job("review")
	if job.State != record.StateBlocked || job.Verdict != record.VerdictFailed {
		t.Fatalf("job = %s/%s", job.State, job.Verdict)
	}
	if !strings.HasPrefix(job.Alert, "blocked label: github: label aivara-se/dispatch#212: 403") {
		t.Fatalf("alert = %q", job.Alert)
	}
	if h.stub.calls != 1 {
		t.Fatalf("label calls = %d", h.stub.calls)
	}
	if len(h.alerts) == 0 || h.alerts[len(h.alerts)-1] == "" {
		t.Fatalf("the event carried no alert: %v", h.alerts)
	}
	if deliveries := h.count("deliveries"); deliveries != 2 {
		t.Fatalf("deliveries = %d", deliveries)
	}
}

func TestReopenedRecordFindsTheJob(t *testing.T) {
	h := newHarness(t, map[string]string{"review": reviewFlow})
	if status := h.post(delivery{action: "opened", number: 212}); status != http.StatusAccepted {
		t.Fatalf("status = %d", status)
	}
	if err := h.store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := record.Open(h.dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer store.Close()
	job, err := store.Job(context.Background(), "review", "aivara-se", "dispatch", 212, "")
	if err != nil {
		t.Fatalf("job after reopen: %v", err)
	}
	if job.Key != "212" || job.State != record.StateStarted {
		t.Fatalf("job = %+v", job)
	}
	if jobs := h.count("jobs"); jobs != 1 {
		t.Fatalf("jobs = %d", jobs)
	}
}

func TestTwoFlowsStartTwoJobs(t *testing.T) {
	h := newHarness(t, map[string]string{"review": reviewFlow, "audit": auditFlow})
	if status := h.post(delivery{action: "opened", number: 212}); status != http.StatusAccepted {
		t.Fatalf("status = %d", status)
	}
	if jobs, deliveries := h.count("jobs"), h.count("deliveries"); jobs != 2 || deliveries != 1 {
		t.Fatalf("jobs = %d, deliveries = %d", jobs, deliveries)
	}
	if h.job("review").Key != "212" || h.job("audit").Key != "audit-212" {
		t.Fatalf("keys = %q, %q", h.job("review").Key, h.job("audit").Key)
	}
}

func TestUnmergedCloseLabelsThroughTheApp(t *testing.T) {
	var labeled bool
	var labelBody string
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/aivara-se/dispatch/installation":
			w.Write([]byte(`{"id":9}`))
		case "/app/installations/9/access_tokens":
			w.Write([]byte(`{"token":"installation-token","expires_at":"` + time.Now().Add(time.Hour).Format(time.RFC3339) + `"}`))
		case "/repos/aivara-se/dispatch/issues/212/labels":
			labeled = true
			body, _ := io.ReadAll(r.Body)
			labelBody = string(body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer appServer.Close()

	app, err := github.New(123456, testKey(t), appServer.URL, appServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	h := newHarnessWith(t, map[string]string{"review": reviewFlow}, app)
	if status := h.post(delivery{id: "open-1", action: "opened", number: 212}); status != http.StatusAccepted {
		t.Fatalf("open status = %d", status)
	}
	if status := h.post(delivery{id: "close-1", action: "closed", number: 212}); status != http.StatusAccepted {
		t.Fatalf("close status = %d", status)
	}
	if !labeled {
		t.Fatal("the close wrote no label")
	}
	if labelBody != `{"labels":["blocked"]}` {
		t.Fatalf("label body = %q", labelBody)
	}
	job := h.job("review")
	if job.State != record.StateBlocked || job.Verdict != record.VerdictFailed || job.Alert != "" {
		t.Fatalf("job = %s/%s alert=%q", job.State, job.Verdict, job.Alert)
	}
}

func testKey(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func sign(key, body []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestReviewRequestedWakesTheLoginAgent(t *testing.T) {
	h := newHarness(t, map[string]string{"reviewer": reviewerFlow})
	if status := h.post(delivery{action: "review_requested", logins: []string{"momo-bot"}, number: 212}); status != http.StatusAccepted {
		t.Fatalf("status = %d", status)
	}
	job := h.job("reviewer", "momo")
	if job.Key != "212-momo" || job.Agent != "momo" || job.Task != "review" {
		t.Fatalf("job = %+v", job)
	}
	if job.State != record.StateStarted || job.Verdict != record.VerdictPending {
		t.Fatalf("state = %s/%s", job.State, job.Verdict)
	}
	if len(h.started) != 1 || h.started[0] != "212-momo" {
		t.Fatalf("started = %v", h.started)
	}
	if _, err := h.store.Job(context.Background(), "reviewer", "aivara-se", "dispatch", 212, ""); !errors.Is(err, record.ErrNotFound) {
		t.Fatalf("a job under no agent: %v", err)
	}
}

func TestTwoReviewersGetTwoJobs(t *testing.T) {
	h := newHarness(t, map[string]string{"reviewer": reviewerFlow})
	h.post(delivery{id: "d1", action: "review_requested", logins: []string{"momo-bot"}, number: 212})
	h.post(delivery{id: "d2", action: "review_requested", logins: []string{"memo-bot"}, number: 212})
	momo, memo := h.job("reviewer", "momo"), h.job("reviewer", "memo")
	if momo.Key != "212-momo" || memo.Key != "212-memo" {
		t.Fatalf("keys = %q, %q", momo.Key, memo.Key)
	}
	if jobs := h.count("jobs"); jobs != 2 {
		t.Fatalf("jobs = %d", jobs)
	}
	if len(h.started) != 2 {
		t.Fatalf("started = %v", h.started)
	}
}

func TestOpenedWithReviewersWakesEach(t *testing.T) {
	h := newHarness(t, map[string]string{"reviewer": reviewerFlow})
	if status := h.post(delivery{action: "opened", logins: []string{"memo-bot", "momo-bot"}, number: 212}); status != http.StatusAccepted {
		t.Fatalf("status = %d", status)
	}
	if jobs := h.count("jobs"); jobs != 2 {
		t.Fatalf("jobs = %d", jobs)
	}
	if len(h.started) != 2 {
		t.Fatalf("started = %v", h.started)
	}
}

func TestAssignedWakesTheAssignee(t *testing.T) {
	h := newHarness(t, map[string]string{"assign": assignFlow})
	if status := h.post(delivery{action: "assigned", logins: []string{"memo-bot"}, number: 212}); status != http.StatusAccepted {
		t.Fatalf("status = %d", status)
	}
	job := h.job("assign", "memo")
	if job.Key != "212-memo" || job.Task != "work" {
		t.Fatalf("job = %+v", job)
	}
	if len(h.started) != 1 || h.started[0] != "212-memo" {
		t.Fatalf("started = %v", h.started)
	}
}

func TestUnheldLoginStartsNothing(t *testing.T) {
	h := newHarness(t, map[string]string{"reviewer": reviewerFlow})
	if status := h.post(delivery{action: "review_requested", logins: []string{"stranger"}, number: 212}); status != http.StatusAccepted {
		t.Fatalf("status = %d", status)
	}
	if jobs, deliveries := h.count("jobs"), h.count("deliveries"); jobs != 0 || deliveries != 0 {
		t.Fatalf("jobs = %d, deliveries = %d", jobs, deliveries)
	}
}

func TestASecondRequestForOneAgentStartsOneJob(t *testing.T) {
	h := newHarness(t, map[string]string{"reviewer": reviewerFlow})
	h.post(delivery{id: "d1", action: "review_requested", logins: []string{"momo-bot"}, number: 212})
	h.post(delivery{id: "d2", action: "review_requested", logins: []string{"momo-bot"}, number: 212})
	if jobs, deliveries := h.count("jobs"), h.count("deliveries"); jobs != 1 || deliveries != 2 {
		t.Fatalf("jobs = %d, deliveries = %d", jobs, deliveries)
	}
}

// A crash between the job insert and the delivery's claim leaves a job and no
// row, so the redelivery must find that job rather than start a second one.
func TestCrashAfterTheJobInsertReplaysToTheSameJob(t *testing.T) {
	h := newHarness(t, map[string]string{"reviewer": reviewerFlow})
	if _, _, err := h.store.EnsureJob(context.Background(), record.Start{
		Flow: "reviewer", Owner: "aivara-se", Repo: "dispatch", Number: 212,
		Key: "212-momo", Agent: "momo", Task: "review", Title: "a title",
	}); err != nil {
		t.Fatal(err)
	}
	if status := h.post(delivery{action: "review_requested", logins: []string{"momo-bot"}, number: 212}); status != http.StatusAccepted {
		t.Fatalf("status = %d", status)
	}
	if jobs, deliveries := h.count("jobs"), h.count("deliveries"); jobs != 1 || deliveries != 1 {
		t.Fatalf("jobs = %d, deliveries = %d", jobs, deliveries)
	}
	if len(h.started) != 0 {
		t.Fatalf("the replay started a job again: %v", h.started)
	}
}
