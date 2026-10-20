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

func reviewBody(action, sender, reviewer string) string {
	return fmt.Sprintf(`{
		"action": %q,
		"repository": {"full_name": "aivara-se/dispatch"},
		"pull_request": {"number": 105},
		"requested_reviewer": {"login": %q},
		"sender": {"login": %q}
	}`, action, reviewer, sender)
}

func changesBody(action, state, sender, author string) string {
	return fmt.Sprintf(`{
		"action": %q,
		"repository": {"full_name": "aivara-se/dispatch"},
		"pull_request": {"number": 105, "user": {"login": %q}},
		"review": {"state": %q},
		"sender": {"login": %q}
	}`, action, author, state, sender)
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
