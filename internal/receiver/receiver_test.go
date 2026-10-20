package receiver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aivara-se/dispatch/internal/audit"
	"github.com/aivara-se/dispatch/internal/run"
)

const testSecret = "fixture-secret-not-a-credential"

// TestMain lets the test binary act as the fake hermes a run starts: the
// runner is pointed at os.Args[0], and the child re-entry writes the argv it
// was given instead of running tests.
func TestMain(m *testing.M) {
	if out := os.Getenv("DISPATCH_TEST_ARGV"); out != "" && len(os.Args) > 1 && os.Args[1] == "--profile" {
		if err := os.WriteFile(out, []byte(strings.Join(os.Args[1:], "\n")), 0o600); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type call struct {
	profile string
	reason  string
}

type stubRunner struct {
	mu    sync.Mutex
	calls []call
	err   error
}

func (s *stubRunner) Start(profile, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call{profile: profile, reason: reason})
	return s.err
}

type memAudit struct {
	mu      sync.Mutex
	records []audit.Record
}

func (m *memAudit) Append(r audit.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, r)
	return nil
}

func newReceiver(runner Runner) (*Receiver, *memAudit) {
	recorder := &memAudit{}
	agents := map[string]string{"thani-sh-meme": "meme", "thani-sh-mama": "mama"}
	return New([]byte(testSecret), agents, runner, recorder), recorder
}

func sign(body string) string {
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func post(rc *Receiver, event, delivery, body string, signature string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/github", strings.NewReader(body))
	if event != "" {
		req.Header.Set("X-GitHub-Event", event)
	}
	if delivery != "" {
		req.Header.Set("X-GitHub-Delivery", delivery)
	}
	if signature != "" {
		req.Header.Set("X-Hub-Signature-256", signature)
	}
	w := httptest.NewRecorder()
	rc.ServeHTTP(w, req)
	return w
}

const testRepo = "aivara-se/dispatch"

func reviewBody(action, sender, reviewer string) string {
	return reviewBodyAt(testRepo, 105, action, sender, reviewer)
}

func reviewBodyOn(number int, action, sender, reviewer string) string {
	return reviewBodyAt(testRepo, number, action, sender, reviewer)
}

func reviewBodyAt(repository string, number int, action, sender, reviewer string) string {
	return fmt.Sprintf(`{
		"action": %q,
		"repository": {"full_name": %q},
		"pull_request": {"number": %d},
		"requested_reviewer": {"login": %q},
		"sender": {"login": %q}
	}`, action, repository, number, reviewer, sender)
}

func changesBody(action, state, sender, author string) string {
	return changesBodyAt(testRepo, 105, action, state, sender, author)
}

func changesBodyOn(number int, action, state, sender, author string) string {
	return changesBodyAt(testRepo, number, action, state, sender, author)
}

func changesBodyAt(repository string, number int, action, state, sender, author string) string {
	return fmt.Sprintf(`{
		"action": %q,
		"repository": {"full_name": %q},
		"pull_request": {"number": %d, "user": {"login": %q}},
		"review": {"state": %q},
		"sender": {"login": %q}
	}`, action, repository, number, author, state, sender)
}

func issueBody(action, sender, assignee string) string {
	return issueBodyOn(124, action, sender, assignee)
}

func issueBodyOn(number int, action, sender, assignee string) string {
	return fmt.Sprintf(`{
		"action": %q,
		"repository": {"full_name": %q},
		"issue": {"number": %d},
		"assignee": {"login": %q},
		"sender": {"login": %q}
	}`, action, testRepo, number, assignee, sender)
}

func prAssignBody(action, sender, assignee string) string {
	return fmt.Sprintf(`{
		"action": %q,
		"repository": {"full_name": %q},
		"pull_request": {"number": 105},
		"assignee": {"login": %q},
		"sender": {"login": %q}
	}`, action, testRepo, assignee, sender)
}

func workflowRunBody(action, conclusion, actor string, numbers ...int) string {
	pullRequests := make([]string, len(numbers))
	for i, number := range numbers {
		pullRequests[i] = fmt.Sprintf(`{"number": %d}`, number)
	}
	return fmt.Sprintf(`{
		"action": %q,
		"repository": {"full_name": %q},
		"workflow_run": {
			"conclusion": %q,
			"actor": {"login": %q},
			"pull_requests": [%s]
		},
		"sender": {"login": %q}
	}`, action, testRepo, conclusion, actor, strings.Join(pullRequests, ","), actor)
}

func commentBody(number int, action, association, sender, text string) string {
	return fmt.Sprintf(`{
		"action": %q,
		"repository": {"full_name": %q},
		"issue": {"number": %d},
		"comment": {"body": %q, "author_association": %q},
		"sender": {"login": %q}
	}`, action, testRepo, number, text, association, sender)
}

// reviewDelivery is a pull_request_review delivery on pull request 105 whose
// action and review state the caller varies.
type reviewDelivery struct {
	action    string
	review    string
	state     string
	draft     bool
	autoMerge string
	sender    string
	author    string
}

func (d reviewDelivery) body() string {
	return fmt.Sprintf(`{
		"action": %q,
		"repository": {"full_name": %q},
		"pull_request": {
			"number": 105,
			"state": %q,
			"draft": %t,
			"auto_merge": %s,
			"user": {"login": %q}
		},
		"review": {"state": %q},
		"sender": {"login": %q}
	}`, d.action, testRepo, d.state, d.draft, d.autoMerge, d.author, d.review, d.sender)
}

// approvalBody is a submitted approval on pull request 105.
func approvalBody(state string, draft bool, autoMerge, sender, author string) string {
	return reviewDelivery{"submitted", "approved", state, draft, autoMerge, sender, author}.body()
}

func readyBody(sender string, reviewers ...string) string {
	entries := make([]string, len(reviewers))
	for i, login := range reviewers {
		entries[i] = fmt.Sprintf(`{"login": %q}`, login)
	}
	return fmt.Sprintf(`{
		"action": "ready_for_review",
		"repository": {"full_name": %q},
		"pull_request": {"number": 105, "requested_reviewers": [%s]},
		"sender": {"login": %q}
	}`, testRepo, strings.Join(entries, ","), sender)
}

func TestSignatureRefusedBeforeParsing(t *testing.T) {
	// The body is not JSON: a 400 here would mean it had been parsed.
	const body = `{"action":`

	otherSignature := func(s string) string {
		mac := hmac.New(sha256.New, []byte("another-secret"))
		mac.Write([]byte(s))
		return "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}

	cases := []struct {
		name      string
		signature string
	}{
		{"missing", ""},
		{"scheme", "sha1=abcd"},
		{"not hex", "sha256=zzzz"},
		{"wrong secret", otherSignature(body)},
		{"truncated", sign(body)[:20]},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc, recorder := newReceiver(&stubRunner{})
			w := post(rc, "pull_request", "d1", body, tc.signature)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
			if len(recorder.records) != 0 {
				t.Fatalf("audit records = %d, want 0", len(recorder.records))
			}
		})
	}
}

func TestBodyRefusedBeforeDecision(t *testing.T) {
	t.Run("oversized", func(t *testing.T) {
		rc, recorder := newReceiver(&stubRunner{})
		body := strings.Repeat("a", MaxBody+1)
		w := post(rc, "pull_request", "d1", body, sign(body))

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
		if len(recorder.records) != 0 {
			t.Fatalf("audit records = %d, want 0", len(recorder.records))
		}
	})

	t.Run("not json", func(t *testing.T) {
		rc, recorder := newReceiver(&stubRunner{})
		body := `payload={"action":"review_requested"}`
		w := post(rc, "pull_request", "d1", body, sign(body))

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
		if len(recorder.records) != 0 {
			t.Fatalf("audit records = %d, want 0", len(recorder.records))
		}
	})
}

func TestEventFilter(t *testing.T) {
	cases := []struct {
		name  string
		event string
		body  string
	}{
		{"ping", "ping", `{"zen": "anything"}`},
		{"another action", "pull_request", reviewBody("opened", "thani-sh", "thani-sh-meme")},
		{"another event", "issues", reviewBody("review_requested", "thani-sh", "thani-sh-meme")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &stubRunner{}
			rc, recorder := newReceiver(runner)
			w := post(rc, tc.event, "d1", tc.body, sign(tc.body))

			if w.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", w.Code)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("runs started = %d, want 0", len(runner.calls))
			}
			if len(recorder.records) != 1 || recorder.records[0].Decision != audit.Ignored {
				t.Fatalf("audit = %+v, want one ignored record", recorder.records)
			}
		})
	}
}

func TestDedup(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	body := reviewBody("review_requested", "thani-sh", "thani-sh-meme")

	for i := 0; i < 2; i++ {
		if w := post(rc, "pull_request", "d1", body, sign(body)); w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202", w.Code)
		}
	}

	if len(runner.calls) != 1 {
		t.Fatalf("runs started = %d, want 1", len(runner.calls))
	}
	if len(recorder.records) != 2 {
		t.Fatalf("audit records = %d, want 2", len(recorder.records))
	}
	if recorder.records[1].Decision != audit.Duplicate {
		t.Fatalf("second decision = %q, want duplicate", recorder.records[1].Decision)
	}
}

func TestIssueAssignmentDedup(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	body := issueBody("assigned", "thani-sh", "thani-sh-meme")

	for i := 0; i < 2; i++ {
		if w := post(rc, "issues", "d1", body, sign(body)); w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202", w.Code)
		}
	}

	if len(runner.calls) != 1 {
		t.Fatalf("runs started = %d, want 1", len(runner.calls))
	}
	if got := recorder.records[1].Decision; got != audit.Duplicate {
		t.Fatalf("second decision = %q, want duplicate", got)
	}
}

func TestSecondRequestStartsSecondRun(t *testing.T) {
	runner := &stubRunner{}
	rc, _ := newReceiver(runner)
	body := reviewBody("review_requested", "thani-sh", "thani-sh-meme")

	for _, delivery := range []string{"d1", "d2"} {
		if w := post(rc, "pull_request", delivery, body, sign(body)); w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202", w.Code)
		}
	}

	if len(runner.calls) != 2 {
		t.Fatalf("runs started = %d, want 2", len(runner.calls))
	}
	if runner.calls[0] != runner.calls[1] {
		t.Fatalf("calls differ: %+v", runner.calls)
	}
}

func TestActorIsNeverWoken(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	body := reviewBody("review_requested", "thani-sh-meme", "thani-sh-meme")

	if w := post(rc, "pull_request", "d1", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runs started = %d, want 0", len(runner.calls))
	}
	if recorder.records[0].Decision != audit.Ignored {
		t.Fatalf("decision = %q, want ignored", recorder.records[0].Decision)
	}
}

func TestChangesRequestedWakesAuthor(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	body := changesBody("submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama")

	if w := post(rc, "pull_request_review", "d1", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	want := call{profile: "mama", reason: "Dispatch: changes requested on aivara-se/dispatch#105"}
	if len(runner.calls) != 1 || runner.calls[0] != want {
		t.Fatalf("calls = %+v, want %+v", runner.calls, want)
	}
	if recorder.records[0].Decision != audit.Started {
		t.Fatalf("decision = %q, want started", recorder.records[0].Decision)
	}
}

func TestIssueAssignmentWakesAssignee(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	// The assignees array names a second configured agent ahead of the
	// assignee; only the assignee object is the wake set.
	body := `{
		"action": "assigned",
		"repository": {"full_name": "aivara-se/dispatch"},
		"issue": {"number": 124},
		"assignee": {"login": "thani-sh-meme"},
		"assignees": [{"login": "thani-sh-mama"}, {"login": "thani-sh-meme"}],
		"sender": {"login": "thani-sh"}
	}`

	if w := post(rc, "issues", "d1", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	want := call{profile: "meme", reason: "Dispatch: issue assigned on aivara-se/dispatch#124"}
	if len(runner.calls) != 1 || runner.calls[0] != want {
		t.Fatalf("calls = %+v, want %+v", runner.calls, want)
	}
	got := recorder.records[0]
	if got.Decision != audit.Started || got.Number != 124 {
		t.Fatalf("record = %+v, want started on 124", got)
	}
}

func TestPullRequestAssignmentWakesAssignee(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	// The assignees array names a second configured agent ahead of the
	// assignee; only the assignee object is the wake set.
	body := `{
		"action": "assigned",
		"repository": {"full_name": "aivara-se/dispatch"},
		"pull_request": {"number": 105},
		"assignee": {"login": "thani-sh-meme"},
		"assignees": [{"login": "thani-sh-mama"}, {"login": "thani-sh-meme"}],
		"sender": {"login": "thani-sh"}
	}`

	if w := post(rc, "pull_request", "d1", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	want := call{profile: "meme", reason: "Dispatch: pull request assigned on aivara-se/dispatch#105"}
	if len(runner.calls) != 1 || runner.calls[0] != want {
		t.Fatalf("calls = %+v, want %+v", runner.calls, want)
	}
	got := recorder.records[0]
	if got.Decision != audit.Started || got.Number != 105 {
		t.Fatalf("record = %+v, want started on 105", got)
	}
}

func TestPullRequestAssignmentWakesNobody(t *testing.T) {
	cases := []struct {
		name string
		body string
		why  string
	}{
		{"unconfigured assignee", prAssignBody("assigned", "thani-sh", "thani-sh-missing"), "not a configured agent"},
		{"self assigned", prAssignBody("assigned", "thani-sh-meme", "thani-sh-meme"), "actor"},
		{"opened", prAssignBody("opened", "thani-sh", "thani-sh-meme"), "is not a delivery that acts"},
		{"edited", prAssignBody("edited", "thani-sh", "thani-sh-meme"), "is not a delivery that acts"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &stubRunner{}
			rc, recorder := newReceiver(runner)

			if w := post(rc, "pull_request", "d1", tc.body, sign(tc.body)); w.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", w.Code)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("runs started = %d, want 0", len(runner.calls))
			}
			got := recorder.records[0]
			if got.Decision != audit.Ignored || !strings.Contains(got.Why, tc.why) {
				t.Fatalf("record = %+v, want ignored because %q", got, tc.why)
			}
		})
	}
}

func TestFailedCheckWakesPusher(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	// The sender is the actor: 010 overrides the actor exclusion here. Two pull
	// requests in one run: the first is the pointer, and only one run starts.
	body := workflowRunBody("completed", "failure", "thani-sh-meme", 105, 106)

	if w := post(rc, "workflow_run", "d1", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	want := call{profile: "meme", reason: "Dispatch: checks failed on aivara-se/dispatch#105"}
	if len(runner.calls) != 1 || runner.calls[0] != want {
		t.Fatalf("calls = %+v, want %+v", runner.calls, want)
	}
	got := recorder.records[0]
	if got.Decision != audit.Started || got.Number != 105 {
		t.Fatalf("record = %+v, want started on 105", got)
	}
}

func TestFailedCheckWakesNobody(t *testing.T) {
	cases := []struct {
		name string
		body string
		why  string
	}{
		{"success", workflowRunBody("completed", "success", "thani-sh-meme", 105), "is not a delivery that acts"},
		{"cancelled", workflowRunBody("completed", "cancelled", "thani-sh-meme", 105), "is not a delivery that acts"},
		{"timed out", workflowRunBody("completed", "timed_out", "thani-sh-meme", 105), "is not a delivery that acts"},
		{"skipped", workflowRunBody("completed", "skipped", "thani-sh-meme", 105), "is not a delivery that acts"},
		{"no pull request", workflowRunBody("completed", "failure", "thani-sh-meme"), "is not a delivery that acts"},
		{"another action", workflowRunBody("requested", "failure", "thani-sh-meme", 105), "is not a delivery that acts"},
		{"unconfigured actor", workflowRunBody("completed", "failure", "thani-sh-missing", 105), "not a configured agent"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &stubRunner{}
			rc, recorder := newReceiver(runner)

			if w := post(rc, "workflow_run", "d1", tc.body, sign(tc.body)); w.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", w.Code)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("runs started = %d, want 0", len(runner.calls))
			}
			got := recorder.records[0]
			if got.Decision != audit.Ignored || !strings.Contains(got.Why, tc.why) {
				t.Fatalf("record = %+v, want ignored because %q", got, tc.why)
			}
		})
	}
}

func TestFailedCheckStopsAfterFiveWakes(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)

	// A failure that wakes nobody spends no budget.
	unconfigured := workflowRunBody("completed", "failure", "thani-sh-missing", 105)
	if w := post(rc, "workflow_run", "unconfigured", unconfigured, sign(unconfigured)); w.Code != http.StatusAccepted {
		t.Fatalf("unconfigured: status = %d, want 202", w.Code)
	}

	body := workflowRunBody("completed", "failure", "thani-sh-meme", 105)
	for i := 1; i <= MaxFailures; i++ {
		if w := post(rc, "workflow_run", fmt.Sprintf("f%d", i), body, sign(body)); w.Code != http.StatusAccepted {
			t.Fatalf("wake %d: status = %d, want 202", i, w.Code)
		}
	}
	if len(runner.calls) != MaxFailures {
		t.Fatalf("runs started = %d, want %d", len(runner.calls), MaxFailures)
	}

	if w := post(rc, "workflow_run", "capped", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("sixth: status = %d, want 202", w.Code)
	}
	last := recorder.records[len(recorder.records)-1]
	if last.Decision != audit.Ignored || !strings.Contains(last.Why, "cap") {
		t.Fatalf("record = %+v, want ignored for the cap", last)
	}
	if len(runner.calls) != MaxFailures {
		t.Fatalf("runs started = %d, want %d after the cap", len(runner.calls), MaxFailures)
	}
}

// TestFailedChecksDoNotSpendTheReviewCap pins the second counter: five failing
// runs leave the ten-round review budget untouched.
func TestFailedChecksDoNotSpendTheReviewCap(t *testing.T) {
	runner := &stubRunner{}
	rc, _ := newReceiver(runner)
	failure := workflowRunBody("completed", "failure", "thani-sh-meme", 105)

	for i := 1; i <= MaxFailures; i++ {
		if w := post(rc, "workflow_run", fmt.Sprintf("f%d", i), failure, sign(failure)); w.Code != http.StatusAccepted {
			t.Fatalf("failure %d: status = %d, want 202", i, w.Code)
		}
	}

	round := changesBody("submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama")
	for i := 1; i <= MaxRounds; i++ {
		if w := post(rc, "pull_request_review", fmt.Sprintf("r%d", i), round, sign(round)); w.Code != http.StatusAccepted {
			t.Fatalf("round %d: status = %d, want 202", i, w.Code)
		}
	}
	if len(runner.calls) != MaxFailures+MaxRounds {
		t.Fatalf("runs started = %d, want %d", len(runner.calls), MaxFailures+MaxRounds)
	}
}

// TestFailedCheckIsNotBlockedByTheReviewCap pins the guard on the round count:
// a pull request at ten rounds still wakes its pusher on a failure.
func TestFailedCheckIsNotBlockedByTheReviewCap(t *testing.T) {
	runner := &stubRunner{}
	rc, _ := newReceiver(runner)
	round := changesBody("submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama")

	for i := 1; i <= MaxRounds; i++ {
		if w := post(rc, "pull_request_review", fmt.Sprintf("r%d", i), round, sign(round)); w.Code != http.StatusAccepted {
			t.Fatalf("round %d: status = %d, want 202", i, w.Code)
		}
	}

	failure := workflowRunBody("completed", "failure", "thani-sh-meme", 105)
	if w := post(rc, "workflow_run", "failure", failure, sign(failure)); w.Code != http.StatusAccepted {
		t.Fatalf("failure: status = %d, want 202", w.Code)
	}
	if len(runner.calls) != MaxRounds+1 {
		t.Fatalf("runs started = %d, want %d", len(runner.calls), MaxRounds+1)
	}
}

func TestMentionWakesEachAgentOnce(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	body := commentBody(124, "created", "MEMBER", "thani-sh",
		"@thani-sh-meme and @Thani-Sh-Mama; @thani-sh-meme again.")

	if w := post(rc, "issue_comment", "d1", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	want := []call{
		{profile: "mama", reason: "Dispatch: mentioned on aivara-se/dispatch#124"},
		{profile: "meme", reason: "Dispatch: mentioned on aivara-se/dispatch#124"},
	}
	if len(runner.calls) != len(want) {
		t.Fatalf("calls = %+v, want %+v", runner.calls, want)
	}
	for i := range want {
		if runner.calls[i] != want[i] {
			t.Fatalf("calls[%d] = %+v, want %+v", i, runner.calls[i], want[i])
		}
	}
	got := recorder.records[0]
	if got.Decision != audit.Started || got.Profile != "mama,meme" || got.Number != 124 {
		t.Fatalf("record = %+v, want started on 124 by mama,meme", got)
	}
}

func TestMentionMatchIsBounded(t *testing.T) {
	cases := []struct {
		text string
		wake bool
	}{
		{"@thani-sh-mama-2", false},
		{"x@thani-sh-meme", false},
		{"@thani-sh-memex", false},
		{"(@thani-sh-meme)", true},
		{"@THANI-SH-MEME", true},
	}

	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			runner := &stubRunner{}
			rc, recorder := newReceiver(runner)
			body := commentBody(124, "created", "MEMBER", "thani-sh", tc.text)

			if w := post(rc, "issue_comment", "d1", body, sign(body)); w.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", w.Code)
			}
			if tc.wake {
				if len(runner.calls) != 1 || runner.calls[0].profile != "meme" {
					t.Fatalf("calls = %+v, want one run for meme", runner.calls)
				}
				return
			}
			if len(runner.calls) != 0 {
				t.Fatalf("runs started = %d, want 0", len(runner.calls))
			}
			got := recorder.records[0]
			if got.Decision != audit.Ignored || !strings.Contains(got.Why, "no configured agent was mentioned") {
				t.Fatalf("record = %+v, want ignored for no mention", got)
			}
		})
	}
}

func TestMentionNeedsAnAssociation(t *testing.T) {
	const text = "@thani-sh-meme"

	for _, association := range []string{"OWNER", "MEMBER", "COLLABORATOR"} {
		t.Run(association, func(t *testing.T) {
			runner := &stubRunner{}
			rc, _ := newReceiver(runner)
			body := commentBody(124, "created", association, "thani-sh", text)

			if w := post(rc, "issue_comment", "d1", body, sign(body)); w.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", w.Code)
			}
			if len(runner.calls) != 1 {
				t.Fatalf("runs started = %d, want 1", len(runner.calls))
			}
		})
	}

	for _, association := range []string{"CONTRIBUTOR", "NONE", "FIRST_TIMER", ""} {
		t.Run("denied/"+association, func(t *testing.T) {
			runner := &stubRunner{}
			rc, recorder := newReceiver(runner)
			body := commentBody(124, "created", association, "thani-sh", text)

			if w := post(rc, "issue_comment", "d1", body, sign(body)); w.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", w.Code)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("runs started = %d, want 0", len(runner.calls))
			}
			got := recorder.records[0]
			if got.Decision != audit.Ignored || !strings.Contains(got.Why, "association") {
				t.Fatalf("record = %+v, want ignored for the association", got)
			}
		})
	}
}

func TestMentionSkipsItsActor(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	body := commentBody(124, "created", "MEMBER", "thani-sh-meme", "@thani-sh-meme")

	if w := post(rc, "issue_comment", "d1", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runs started = %d, want 0", len(runner.calls))
	}
	got := recorder.records[0]
	if got.Decision != audit.Ignored || !strings.Contains(got.Why, "actor") {
		t.Fatalf("record = %+v, want ignored for the actor", got)
	}
}

func TestMentionIgnoresOtherActions(t *testing.T) {
	for _, action := range []string{"edited", "deleted"} {
		t.Run(action, func(t *testing.T) {
			runner := &stubRunner{}
			rc, recorder := newReceiver(runner)
			body := commentBody(124, action, "MEMBER", "thani-sh", "@thani-sh-meme")

			if w := post(rc, "issue_comment", "d1", body, sign(body)); w.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", w.Code)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("runs started = %d, want 0", len(runner.calls))
			}
			got := recorder.records[0]
			if got.Decision != audit.Ignored || !strings.Contains(got.Why, "is not a delivery that acts") {
				t.Fatalf("record = %+v, want ignored for the action", got)
			}
		})
	}
}

func TestMentionDedup(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	body := commentBody(124, "created", "MEMBER", "thani-sh", "@thani-sh-meme")

	for i := 0; i < 2; i++ {
		if w := post(rc, "issue_comment", "d1", body, sign(body)); w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202", w.Code)
		}
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runs started = %d, want 1", len(runner.calls))
	}
	if got := recorder.records[1].Decision; got != audit.Duplicate {
		t.Fatalf("second decision = %q, want duplicate", got)
	}
}

// TestMentionIgnoresTheReviewCap pins that a mention has no count: a pull
// request at ten rounds still wakes an agent named in a comment.
func TestMentionIgnoresTheReviewCap(t *testing.T) {
	runner := &stubRunner{}
	rc, _ := newReceiver(runner)
	round := changesBodyOn(105, "submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama")

	for i := 1; i <= MaxRounds; i++ {
		if w := post(rc, "pull_request_review", fmt.Sprintf("r%d", i), round, sign(round)); w.Code != http.StatusAccepted {
			t.Fatalf("round %d: status = %d, want 202", i, w.Code)
		}
	}
	body := commentBody(105, "created", "MEMBER", "thani-sh", "@thani-sh-meme")
	if w := post(rc, "issue_comment", "mention", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("mention: status = %d, want 202", w.Code)
	}
	if len(runner.calls) != MaxRounds+1 {
		t.Fatalf("runs started = %d, want %d", len(runner.calls), MaxRounds+1)
	}
	if got := runner.calls[len(runner.calls)-1].profile; got != "meme" {
		t.Fatalf("mention profile = %q, want meme", got)
	}
}

func TestUnarmedApprovalWakesAuthor(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	body := approvalBody("open", false, "null", "thani-sh-meme", "thani-sh-mama")

	if w := post(rc, "pull_request_review", "d1", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	want := call{profile: "mama", reason: "Dispatch: approved but not armed on aivara-se/dispatch#105"}
	if len(runner.calls) != 1 || runner.calls[0] != want {
		t.Fatalf("calls = %+v, want %+v", runner.calls, want)
	}
	got := recorder.records[0]
	if got.Decision != audit.Started || got.Number != 105 {
		t.Fatalf("record = %+v, want started on 105", got)
	}
}

// TestUnarmedApprovalIgnoresTheReviewCap pins that an approval consults neither
// counter: ten changes-requested rounds leave the cap reached, and an unarmed
// approval on that number still starts a run.
func TestUnarmedApprovalIgnoresTheReviewCap(t *testing.T) {
	runner := &stubRunner{}
	rc, _ := newReceiver(runner)
	round := changesBodyOn(105, "submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama")

	for i := 1; i <= MaxRounds; i++ {
		if w := post(rc, "pull_request_review", fmt.Sprintf("r%d", i), round, sign(round)); w.Code != http.StatusAccepted {
			t.Fatalf("round %d: status = %d, want 202", i, w.Code)
		}
	}
	if w := post(rc, "pull_request_review", "capped", round, sign(round)); w.Code != http.StatusAccepted {
		t.Fatalf("capped round: status = %d, want 202", w.Code)
	}
	if len(runner.calls) != MaxRounds {
		t.Fatalf("runs started = %d, want %d: the cap was not reached", len(runner.calls), MaxRounds)
	}

	body := approvalBody("open", false, "null", "thani-sh-meme", "thani-sh-mama")
	if w := post(rc, "pull_request_review", "approval", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("approval: status = %d, want 202", w.Code)
	}
	if len(runner.calls) != MaxRounds+1 {
		t.Fatalf("runs started = %d, want %d", len(runner.calls), MaxRounds+1)
	}
	want := call{profile: "mama", reason: "Dispatch: approved but not armed on aivara-se/dispatch#105"}
	if got := runner.calls[len(runner.calls)-1]; got != want {
		t.Fatalf("approval call = %+v, want %+v", got, want)
	}
}

// TestApprovalsAddNoRounds pins the other side of that: an approval advances
// neither counter, so ten unarmed approvals on a pull request leave its round
// count at zero and the changes-requested review that follows still wakes.
func TestApprovalsAddNoRounds(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	body := approvalBody("open", false, "null", "thani-sh-meme", "thani-sh-mama")

	for i := 1; i <= MaxRounds; i++ {
		if w := post(rc, "pull_request_review", fmt.Sprintf("approval%d", i), body, sign(body)); w.Code != http.StatusAccepted {
			t.Fatalf("approval %d: status = %d, want 202", i, w.Code)
		}
		if got := recorder.records[len(recorder.records)-1].Decision; got != audit.Started {
			t.Fatalf("approval %d: decision = %q, want started", i, got)
		}
	}
	if len(runner.calls) != MaxRounds {
		t.Fatalf("runs started = %d, want %d", len(runner.calls), MaxRounds)
	}

	round := changesBody("submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama")
	if w := post(rc, "pull_request_review", "round1", round, sign(round)); w.Code != http.StatusAccepted {
		t.Fatalf("changes requested: status = %d, want 202", w.Code)
	}
	if got := recorder.records[len(recorder.records)-1].Decision; got != audit.Started {
		t.Fatalf("changes requested after %d approvals: decision = %q, want started", MaxRounds, got)
	}
	if len(runner.calls) != MaxRounds+1 {
		t.Fatalf("runs started = %d, want %d: an approval spent a round", len(runner.calls), MaxRounds+1)
	}
}

func TestApprovalWakesNobody(t *testing.T) {
	cases := []struct {
		name      string
		action    string
		review    string
		state     string
		draft     bool
		autoMerge string
		sender    string
		author    string
		why       string
	}{
		{"armed", "submitted", "approved", "open", false, `{"merge_method": "squash"}`, "thani-sh-meme", "thani-sh-mama", "is not a delivery that acts"},
		{"draft", "submitted", "approved", "open", true, "null", "thani-sh-meme", "thani-sh-mama", "is not a delivery that acts"},
		{"closed", "submitted", "approved", "closed", false, "null", "thani-sh-meme", "thani-sh-mama", "is not a delivery that acts"},
		{"merged", "submitted", "approved", "merged", false, "null", "thani-sh-meme", "thani-sh-mama", "is not a delivery that acts"},
		{"commented review", "submitted", "commented", "open", false, "null", "thani-sh-meme", "thani-sh-mama", "is not a delivery that acts"},
		{"edited approval", "edited", "approved", "open", false, "null", "thani-sh-meme", "thani-sh-mama", "is not a delivery that acts"},
		{"unconfigured author", "submitted", "approved", "open", false, "null", "thani-sh-meme", "thani-sh-missing", "not a configured agent"},
		{"actor is the author", "submitted", "approved", "open", false, "null", "thani-sh-meme", "thani-sh-meme", "actor"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &stubRunner{}
			rc, recorder := newReceiver(runner)
			body := reviewDelivery{tc.action, tc.review, tc.state, tc.draft, tc.autoMerge, tc.sender, tc.author}.body()

			if w := post(rc, "pull_request_review", "d1", body, sign(body)); w.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", w.Code)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("runs started = %d, want 0", len(runner.calls))
			}
			got := recorder.records[0]
			if got.Decision != audit.Ignored || !strings.Contains(got.Why, tc.why) {
				t.Fatalf("record = %+v, want ignored because %q", got, tc.why)
			}
		})
	}
}

func TestReadyForReviewWakesReviewers(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	body := readyBody("thani-sh", "thani-sh-meme", "thani-sh-mama")

	if w := post(rc, "pull_request", "d1", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	want := []call{
		{profile: "mama", reason: "Dispatch: ready for review on aivara-se/dispatch#105"},
		{profile: "meme", reason: "Dispatch: ready for review on aivara-se/dispatch#105"},
	}
	if len(runner.calls) != len(want) {
		t.Fatalf("calls = %+v, want %+v", runner.calls, want)
	}
	for i := range want {
		if runner.calls[i] != want[i] {
			t.Fatalf("calls[%d] = %+v, want %+v", i, runner.calls[i], want[i])
		}
	}
	got := recorder.records[0]
	if got.Decision != audit.Started || got.Profile != "mama,meme" || got.Number != 105 {
		t.Fatalf("record = %+v, want started on 105 by mama,meme", got)
	}
}

func TestReadyForReviewWakesNobody(t *testing.T) {
	teams := `{
		"action": "ready_for_review",
		"repository": {"full_name": "aivara-se/dispatch"},
		"pull_request": {"number": 105, "requested_reviewers": [], "requested_teams": [{"slug": "reviewers"}]},
		"sender": {"login": "thani-sh"}
	}`
	cases := []struct {
		name string
		body string
	}{
		{"no reviewers", readyBody("thani-sh")},
		{"unconfigured reviewer", readyBody("thani-sh", "thani-sh-missing")},
		{"actor only", readyBody("thani-sh-meme", "thani-sh-meme")},
		{"team only", teams},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &stubRunner{}
			rc, recorder := newReceiver(runner)

			if w := post(rc, "pull_request", "d1", tc.body, sign(tc.body)); w.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", w.Code)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("runs started = %d, want 0", len(runner.calls))
			}
			got := recorder.records[0]
			if got.Decision != audit.Ignored || !strings.Contains(got.Why, "no configured reviewer") {
				t.Fatalf("record = %+v, want ignored for no reviewer", got)
			}
		})
	}
}

func TestReadyForReviewSkipsItsActorAndRepeats(t *testing.T) {
	runner := &stubRunner{}
	rc, _ := newReceiver(runner)
	body := readyBody("thani-sh-meme", "thani-sh-meme", "thani-sh-meme", "thani-sh-mama")

	if w := post(rc, "pull_request", "d1", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if len(runner.calls) != 1 || runner.calls[0].profile != "mama" {
		t.Fatalf("calls = %+v, want one run for mama", runner.calls)
	}
}

// TestReadyForReviewIgnoresTheReviewCap pins that a draft marked ready has no
// count: a pull request at ten rounds still wakes its reviewers.
func TestReadyForReviewIgnoresTheReviewCap(t *testing.T) {
	runner := &stubRunner{}
	rc, _ := newReceiver(runner)
	round := changesBody("submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama")

	for i := 1; i <= MaxRounds; i++ {
		if w := post(rc, "pull_request_review", fmt.Sprintf("r%d", i), round, sign(round)); w.Code != http.StatusAccepted {
			t.Fatalf("round %d: status = %d, want 202", i, w.Code)
		}
	}
	body := readyBody("thani-sh", "thani-sh-meme")
	if w := post(rc, "pull_request", "ready", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("ready: status = %d, want 202", w.Code)
	}
	if len(runner.calls) != MaxRounds+1 {
		t.Fatalf("runs started = %d, want %d", len(runner.calls), MaxRounds+1)
	}
	if got := runner.calls[len(runner.calls)-1].profile; got != "meme" {
		t.Fatalf("ready profile = %q, want meme", got)
	}
}

func TestIssueDeliveryWakesNobody(t *testing.T) {
	cases := []struct {
		name string
		body string
		why  string
	}{
		{"opened", issueBody("opened", "thani-sh", "thani-sh-meme"), "is not a delivery that acts"},
		{"edited", issueBody("edited", "thani-sh", "thani-sh-meme"), "is not a delivery that acts"},
		{"labeled", issueBody("labeled", "thani-sh", "thani-sh-meme"), "is not a delivery that acts"},
		{"unassigned", issueBody("unassigned", "thani-sh", "thani-sh-meme"), "is not a delivery that acts"},
		{"unconfigured assignee", issueBody("assigned", "thani-sh", "thani-sh-missing"), "not a configured agent"},
		{"self assigned", issueBody("assigned", "thani-sh-meme", "thani-sh-meme"), "actor"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &stubRunner{}
			rc, recorder := newReceiver(runner)

			if w := post(rc, "issues", "d1", tc.body, sign(tc.body)); w.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", w.Code)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("runs started = %d, want 0", len(runner.calls))
			}
			if len(recorder.records) != 1 {
				t.Fatalf("audit records = %d, want 1", len(recorder.records))
			}
			got := recorder.records[0]
			if got.Decision != audit.Ignored || !strings.Contains(got.Why, tc.why) {
				t.Fatalf("record = %+v, want ignored because %q", got, tc.why)
			}
		})
	}
}

// TestIssueAssignmentIsNotCapped pins that the cap is read for pull requests
// only: an issue whose number matches a capped pull request still wakes its
// assignee.
func TestIssueAssignmentIsNotCapped(t *testing.T) {
	runner := &stubRunner{}
	rc, _ := newReceiver(runner)
	round := changesBodyOn(124, "submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama")

	for i := 1; i <= MaxRounds; i++ {
		if w := post(rc, "pull_request_review", fmt.Sprintf("d%d", i), round, sign(round)); w.Code != http.StatusAccepted {
			t.Fatalf("round %d: status = %d, want 202", i, w.Code)
		}
	}

	body := issueBodyOn(124, "assigned", "thani-sh", "thani-sh-meme")
	if w := post(rc, "issues", "assignment", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("assignment: status = %d, want 202", w.Code)
	}
	if len(runner.calls) != MaxRounds+1 {
		t.Fatalf("runs started = %d, want %d", len(runner.calls), MaxRounds+1)
	}
	if got := runner.calls[len(runner.calls)-1].profile; got != "meme" {
		t.Fatalf("assignment profile = %q, want meme", got)
	}
}

// TestIssueAssignmentAddsNoRound pins ADR 008's "the assignment adds no
// count": ten assignments on one issue leave its pull request short of the
// cap, so a changes-requested review on that number still wakes the author.
func TestIssueAssignmentAddsNoRound(t *testing.T) {
	runner := &stubRunner{}
	rc, _ := newReceiver(runner)

	body := issueBodyOn(124, "assigned", "thani-sh", "thani-sh-meme")
	for i := 1; i <= MaxRounds; i++ {
		if w := post(rc, "issues", fmt.Sprintf("a%d", i), body, sign(body)); w.Code != http.StatusAccepted {
			t.Fatalf("assignment %d: status = %d, want 202", i, w.Code)
		}
	}
	if len(runner.calls) != MaxRounds {
		t.Fatalf("runs started = %d, want %d", len(runner.calls), MaxRounds)
	}

	round := changesBodyOn(124, "submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama")
	if w := post(rc, "pull_request_review", "after-assignments", round, sign(round)); w.Code != http.StatusAccepted {
		t.Fatalf("review after the assignments: status = %d, want 202", w.Code)
	}
	if len(runner.calls) != MaxRounds+1 {
		t.Fatalf("runs started = %d, want %d", len(runner.calls), MaxRounds+1)
	}
	want := call{profile: "mama", reason: "Dispatch: changes requested on aivara-se/dispatch#124"}
	if got := runner.calls[len(runner.calls)-1]; got != want {
		t.Fatalf("review after the assignments = %+v, want %+v", got, want)
	}
}

func TestCapStopsWakingAfterTenRounds(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	body := changesBody("submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama")

	for i := 1; i <= MaxRounds; i++ {
		if w := post(rc, "pull_request_review", fmt.Sprintf("d%d", i), body, sign(body)); w.Code != http.StatusAccepted {
			t.Fatalf("round %d: status = %d, want 202", i, w.Code)
		}
	}
	if len(runner.calls) != MaxRounds {
		t.Fatalf("runs started = %d, want %d", len(runner.calls), MaxRounds)
	}

	// The eleventh round, and a review request on the capped pull request.
	capped := []struct {
		name  string
		event string
		body  string
	}{
		{"changes requested", "pull_request_review", body},
		{"review requested", "pull_request", reviewBody("review_requested", "thani-sh", "thani-sh-meme")},
	}
	for i, tc := range capped {
		if w := post(rc, tc.event, fmt.Sprintf("capped%d", i), tc.body, sign(tc.body)); w.Code != http.StatusAccepted {
			t.Fatalf("%s: status = %d, want 202", tc.name, w.Code)
		}
		last := recorder.records[len(recorder.records)-1]
		if last.Decision != audit.Ignored || !strings.Contains(last.Why, "cap") {
			t.Fatalf("%s: record = %+v, want ignored for the cap", tc.name, last)
		}
		if last.Profile != "" {
			t.Fatalf("%s: record names a profile: %+v", tc.name, last)
		}
	}
	if len(runner.calls) != MaxRounds {
		t.Fatalf("runs started = %d, want %d after the cap", len(runner.calls), MaxRounds)
	}
}

func TestOnlyChangesRequestedCountsRounds(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)

	for i := 1; i <= MaxRounds-1; i++ {
		body := changesBody("submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama")
		if w := post(rc, "pull_request_review", fmt.Sprintf("d%d", i), body, sign(body)); w.Code != http.StatusAccepted {
			t.Fatalf("round %d: status = %d, want 202", i, w.Code)
		}
	}

	// An approval and a comment add no round, even at the ninth.
	for i, state := range []string{"approved", "commented"} {
		body := changesBody("submitted", state, "thani-sh-meme", "thani-sh-mama")
		if w := post(rc, "pull_request_review", fmt.Sprintf("other%d", i), body, sign(body)); w.Code != http.StatusAccepted {
			t.Fatalf("%s: status = %d, want 202", state, w.Code)
		}
	}

	tenth := changesBody("submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama")
	if w := post(rc, "pull_request_review", "d10", tenth, sign(tenth)); w.Code != http.StatusAccepted {
		t.Fatalf("tenth round: status = %d, want 202", w.Code)
	}
	if len(runner.calls) != MaxRounds {
		t.Fatalf("runs started = %d, want %d", len(runner.calls), MaxRounds)
	}
	if got := recorder.records[len(recorder.records)-1].Decision; got != audit.Started {
		t.Fatalf("tenth round decision = %q, want started", got)
	}
}

// TestReviewRequestsAddNoRounds pins that a review request leaves the round
// count alone: nine of them between a changes requested and the tenth must not
// bring the cap forward.
func TestReviewRequestsAddNoRounds(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	round := changesBody("submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama")
	request := reviewBody("review_requested", "thani-sh", "thani-sh-meme")

	if w := post(rc, "pull_request_review", "round1", round, sign(round)); w.Code != http.StatusAccepted {
		t.Fatalf("first round: status = %d, want 202", w.Code)
	}
	for i := 1; i <= MaxRounds-1; i++ {
		if w := post(rc, "pull_request", fmt.Sprintf("request%d", i), request, sign(request)); w.Code != http.StatusAccepted {
			t.Fatalf("review request %d: status = %d, want 202", i, w.Code)
		}
		if got := recorder.records[len(recorder.records)-1].Decision; got != audit.Started {
			t.Fatalf("review request %d: decision = %q, want started", i, got)
		}
	}
	if len(runner.calls) != MaxRounds {
		t.Fatalf("runs started = %d, want %d before the tenth round", len(runner.calls), MaxRounds)
	}

	// The tenth round still wakes: the review requests spent no count.
	if w := post(rc, "pull_request_review", "round10", round, sign(round)); w.Code != http.StatusAccepted {
		t.Fatalf("tenth round: status = %d, want 202", w.Code)
	}
	if got := recorder.records[len(recorder.records)-1].Decision; got != audit.Started {
		t.Fatalf("tenth round decision = %q, want started", got)
	}
	if len(runner.calls) != MaxRounds+1 {
		t.Fatalf("runs started = %d, want %d", len(runner.calls), MaxRounds+1)
	}
}

// TestCapIsPerRepositoryAndNumber pins that the cap counts one pull request,
// not one number: a repository's ten rounds leave another repository's pull
// request of the same number free to wake.
func TestCapIsPerRepositoryAndNumber(t *testing.T) {
	runner := &stubRunner{}
	rc, _ := newReceiver(runner)
	capped := changesBodyAt("aivara-se/dispatch", 105, "submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama")

	for i := 1; i <= MaxRounds; i++ {
		if w := post(rc, "pull_request_review", fmt.Sprintf("d%d", i), capped, sign(capped)); w.Code != http.StatusAccepted {
			t.Fatalf("round %d: status = %d, want 202", i, w.Code)
		}
	}

	other := reviewBodyAt("aivara-se/other", 105, "review_requested", "thani-sh", "thani-sh-meme")
	if w := post(rc, "pull_request", "other", other, sign(other)); w.Code != http.StatusAccepted {
		t.Fatalf("other repository: status = %d, want 202", w.Code)
	}
	if len(runner.calls) != MaxRounds+1 {
		t.Fatalf("runs started = %d, want %d", len(runner.calls), MaxRounds+1)
	}
	if got := runner.calls[len(runner.calls)-1].profile; got != "meme" {
		t.Fatalf("last run profile = %q, want meme", got)
	}
}

func TestCapIsPerPullRequest(t *testing.T) {
	runner := &stubRunner{}
	rc, _ := newReceiver(runner)
	capped := changesBodyOn(105, "submitted", "changes_requested", "thani-sh-meme", "thani-sh-mama")

	for i := 1; i <= MaxRounds; i++ {
		if w := post(rc, "pull_request_review", fmt.Sprintf("d%d", i), capped, sign(capped)); w.Code != http.StatusAccepted {
			t.Fatalf("round %d: status = %d, want 202", i, w.Code)
		}
	}

	other := reviewBodyOn(106, "review_requested", "thani-sh", "thani-sh-meme")
	if w := post(rc, "pull_request", "other", other, sign(other)); w.Code != http.StatusAccepted {
		t.Fatalf("other pull request: status = %d, want 202", w.Code)
	}
	if len(runner.calls) != MaxRounds+1 {
		t.Fatalf("runs started = %d, want %d", len(runner.calls), MaxRounds+1)
	}
	if got := runner.calls[len(runner.calls)-1].profile; got != "meme" {
		t.Fatalf("last run profile = %q, want meme", got)
	}
}

func TestReviewWithoutChangesWakesNobody(t *testing.T) {
	cases := []struct{ action, state string }{
		{"submitted", "approved"},
		{"submitted", "commented"},
		{"edited", "changes_requested"},
		{"dismissed", "changes_requested"},
	}

	for _, tc := range cases {
		t.Run(tc.action+"/"+tc.state, func(t *testing.T) {
			runner := &stubRunner{}
			rc, recorder := newReceiver(runner)
			body := changesBody(tc.action, tc.state, "thani-sh-meme", "thani-sh-mama")

			if w := post(rc, "pull_request_review", "d1", body, sign(body)); w.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", w.Code)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("runs started = %d, want 0", len(runner.calls))
			}
			if recorder.records[0].Decision != audit.Ignored {
				t.Fatalf("decision = %q, want ignored", recorder.records[0].Decision)
			}
		})
	}
}

func TestUnconfiguredReviewerIsIgnored(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	body := reviewBody("review_requested", "thani-sh", "thani-sh-missing")

	if w := post(rc, "pull_request", "d1", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runs started = %d, want 0", len(runner.calls))
	}
	if recorder.records[0].Decision != audit.Ignored {
		t.Fatalf("decision = %q, want ignored", recorder.records[0].Decision)
	}
}

func TestTeamReviewRequestIsIgnored(t *testing.T) {
	runner := &stubRunner{}
	rc, recorder := newReceiver(runner)
	body := `{
		"action": "review_requested",
		"repository": {"full_name": "aivara-se/dispatch"},
		"pull_request": {"number": 105},
		"requested_team": {"name": "reviewers"},
		"sender": {"login": "thani-sh"}
	}`

	if w := post(rc, "pull_request", "d1", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runs started = %d, want 0", len(runner.calls))
	}
	if recorder.records[0].Decision != audit.Ignored {
		t.Fatalf("decision = %q, want ignored", recorder.records[0].Decision)
	}
}

func TestRunThatCannotStartIsRecorded(t *testing.T) {
	runner := &stubRunner{err: errors.New("exec: no such file")}
	rc, recorder := newReceiver(runner)
	body := reviewBody("review_requested", "thani-sh", "thani-sh-meme")

	if w := post(rc, "pull_request", "d1", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if recorder.records[0].Decision != audit.StartFailed {
		t.Fatalf("decision = %q, want start_failed", recorder.records[0].Decision)
	}
	if recorder.records[0].Why == "" {
		t.Fatal("start_failed record carries no reason")
	}
}

func TestEndToEndSignedPostStartsRun(t *testing.T) {
	const sentinel = "SENTINEL-3f9c"

	reviewRequested := fmt.Sprintf(`{
		"action": "review_requested",
		"repository": {"full_name": "aivara-se/dispatch", "description": %q},
		"pull_request": {
			"number": 105,
			"title": %q,
			"body": %q,
			"head": {"ref": %q},
			"user": {"login": %q}
		},
		"requested_reviewer": {"login": "thani-sh-meme"},
		"sender": {"login": "thani-sh"},
		"comment": {"body": %q},
		"files": [{"filename": %q}]
	}`, sentinel, sentinel, sentinel, sentinel, sentinel, sentinel, sentinel)

	changesRequested := fmt.Sprintf(`{
		"action": "submitted",
		"repository": {"full_name": "aivara-se/dispatch", "description": %q},
		"pull_request": {
			"number": 105,
			"title": %q,
			"body": %q,
			"head": {"ref": %q},
			"user": {"login": "thani-sh-mama"}
		},
		"review": {"state": "changes_requested", "body": %q},
		"sender": {"login": "thani-sh-meme"},
		"files": [{"filename": %q}]
	}`, sentinel, sentinel, sentinel, sentinel, sentinel, sentinel)

	issueAssigned := fmt.Sprintf(`{
		"action": "assigned",
		"repository": {"full_name": "aivara-se/dispatch", "description": %q},
		"issue": {
			"number": 124,
			"title": %q,
			"body": %q
		},
		"assignee": {"login": "thani-sh-meme"},
		"assignees": [{"login": "thani-sh-meme"}],
		"sender": {"login": "thani-sh"},
		"comment": {"body": %q},
		"label": {"name": %q}
	}`, sentinel, sentinel, sentinel, sentinel, sentinel)

	prAssigned := fmt.Sprintf(`{
		"action": "assigned",
		"repository": {"full_name": "aivara-se/dispatch", "description": %q},
		"pull_request": {
			"number": 105,
			"title": %q,
			"body": %q,
			"head": {"ref": %q},
			"user": {"login": "thani-sh-mama"}
		},
		"assignee": {"login": "thani-sh-meme"},
		"assignees": [{"login": "thani-sh-meme"}],
		"sender": {"login": "thani-sh"}
	}`, sentinel, sentinel, sentinel, sentinel)

	failedRun := fmt.Sprintf(`{
		"action": "completed",
		"repository": {"full_name": "aivara-se/dispatch", "description": %q},
		"workflow_run": {
			"conclusion": "failure",
			"name": %q,
			"display_title": %q,
			"head_branch": %q,
			"actor": {"login": "thani-sh-meme"},
			"pull_requests": [{"number": 105}]
		},
		"sender": {"login": "thani-sh-meme"}
	}`, sentinel, sentinel, sentinel, sentinel)

	mentioned := fmt.Sprintf(`{
		"action": "created",
		"repository": {"full_name": "aivara-se/dispatch", "description": %q},
		"issue": {"number": 124, "title": %q, "body": %q},
		"comment": {"body": %q, "author_association": "MEMBER"},
		"sender": {"login": "thani-sh"}
	}`, sentinel, sentinel, sentinel, sentinel+" @thani-sh-meme "+sentinel)

	unarmedApproval := fmt.Sprintf(`{
		"action": "submitted",
		"repository": {"full_name": "aivara-se/dispatch", "description": %q},
		"pull_request": {
			"number": 105,
			"state": "open",
			"draft": false,
			"auto_merge": null,
			"title": %q,
			"body": %q,
			"head": {"ref": %q},
			"user": {"login": "thani-sh-mama"}
		},
		"review": {"state": "approved", "body": %q},
		"sender": {"login": "thani-sh-meme"}
	}`, sentinel, sentinel, sentinel, sentinel, sentinel)

	readyForReview := fmt.Sprintf(`{
		"action": "ready_for_review",
		"repository": {"full_name": "aivara-se/dispatch", "description": %q},
		"pull_request": {
			"number": 105,
			"title": %q,
			"body": %q,
			"head": {"ref": %q},
			"requested_reviewers": [{"login": "thani-sh-meme"}]
		},
		"sender": {"login": "thani-sh"}
	}`, sentinel, sentinel, sentinel, sentinel)

	cases := []struct {
		name    string
		event   string
		body    string
		profile string
		reason  string
	}{
		{
			"review requested", "pull_request", reviewRequested, "meme",
			"Dispatch: review requested on aivara-se/dispatch#105",
		},
		{
			"changes requested", "pull_request_review", changesRequested, "mama",
			"Dispatch: changes requested on aivara-se/dispatch#105",
		},
		{
			"issue assigned", "issues", issueAssigned, "meme",
			"Dispatch: issue assigned on aivara-se/dispatch#124",
		},
		{
			"pull request assigned", "pull_request", prAssigned, "meme",
			"Dispatch: pull request assigned on aivara-se/dispatch#105",
		},
		{
			"checks failed", "workflow_run", failedRun, "meme",
			"Dispatch: checks failed on aivara-se/dispatch#105",
		},
		{
			"mentioned", "issue_comment", mentioned, "meme",
			"Dispatch: mentioned on aivara-se/dispatch#124",
		},
		{
			"approved but not armed", "pull_request_review", unarmedApproval, "mama",
			"Dispatch: approved but not armed on aivara-se/dispatch#105",
		},
		{
			"ready for review", "pull_request", readyForReview, "meme",
			"Dispatch: ready for review on aivara-se/dispatch#105",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := filepath.Join(t.TempDir(), "argv")
			t.Setenv("DISPATCH_TEST_ARGV", capture)

			recorder := &memAudit{}
			agents := map[string]string{"thani-sh-meme": "meme", "thani-sh-mama": "mama"}
			rc := New([]byte(testSecret), agents, run.New(os.Args[0]), recorder)

			if w := post(rc, tc.event, "d1", tc.body, sign(tc.body)); w.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", w.Code)
			}

			var args []string
			deadline := time.Now().Add(5 * time.Second)
			for {
				data, err := os.ReadFile(capture)
				if err == nil && len(data) > 0 {
					args = strings.Split(string(data), "\n")
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("fake hermes captured no argv: %v", err)
				}
				time.Sleep(10 * time.Millisecond)
			}

			want := []string{"--profile", tc.profile, "chat", "-q", tc.reason}
			if len(args) != len(want) {
				t.Fatalf("argv = %q, want %q", args, want)
			}
			for i := range want {
				if args[i] != want[i] {
					t.Fatalf("argv[%d] = %q, want %q", i, args[i], want[i])
				}
				if strings.Contains(args[i], sentinel) {
					t.Fatalf("argv[%d] carries payload text: %q", i, args[i])
				}
			}

			if len(recorder.records) != 1 {
				t.Fatalf("audit records = %d, want 1", len(recorder.records))
			}
			if got := recorder.records[0].Reason; got != want[len(want)-1] {
				t.Fatalf("recorded reason = %q, want %q", got, want[len(want)-1])
			}
		})
	}
}

func TestAuditLinePerDelivery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	record, err := audit.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer record.Close()

	rc := New([]byte(testSecret), map[string]string{"thani-sh-meme": "meme"}, &stubRunner{}, record)

	ping := `{"zen": "anything"}`
	if w := post(rc, "ping", "d1", ping, sign(ping)); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	body := reviewBody("review_requested", "thani-sh", "thani-sh-meme")
	if w := post(rc, "pull_request", "d2", body, sign(body)); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("audit lines = %d, want 2:\n%s", len(lines), data)
	}

	var first, second audit.Record
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}
	if first.Delivery != "d1" || first.Decision != audit.Ignored || first.Answer != http.StatusAccepted {
		t.Fatalf("first record = %+v", first)
	}
	if second.Delivery != "d2" || second.Decision != audit.Started || second.Profile != "meme" {
		t.Fatalf("second record = %+v", second)
	}
	if second.Repository != "aivara-se/dispatch" || second.Number != 105 {
		t.Fatalf("second record identity = %+v", second)
	}
}

func TestSeenExpires(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	cache := newSeen(time.Hour, func() time.Time { return now })

	cache.add("d1")
	if !cache.has("d1") {
		t.Fatal("a delivery added was not seen")
	}

	now = now.Add(time.Hour)
	if cache.has("d1") {
		t.Fatal("a delivery is still seen after the TTL")
	}
}
