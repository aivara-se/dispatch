package webhook_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aivara-se/dispatch/internal/webhook"
)

var secret = []byte("s3cret")

func TestVerify(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	valid := sign(secret, body)

	cases := []struct {
		name      string
		signature string
		want      bool
	}{
		{name: "valid", signature: valid, want: true},
		{name: "another secret", signature: sign([]byte("other"), body), want: false},
		{name: "missing", signature: "", want: false},
		{name: "not hex", signature: "sha256=zz", want: false},
		{name: "sha1", signature: "sha1=" + valid[len("sha256="):], want: false},
		{name: "other body", signature: sign(secret, []byte(`{}`)), want: false},
	}
	for _, tc := range cases {
		if got := webhook.Verify(secret, body, tc.signature); got != tc.want {
			t.Errorf("%s: Verify = %v", tc.name, got)
		}
	}
}

func TestParse(t *testing.T) {
	header := http.Header{}
	header.Set("X-GitHub-Event", webhook.KindPullRequest)
	header.Set("X-GitHub-Delivery", "d1")

	event, err := webhook.Parse(header, []byte(`{
		"action": "opened",
		"repository": { "full_name": "aivara-se/dispatch" },
		"pull_request": { "number": 212, "title": "evict counts", "html_url": "https://github.com/aivara-se/dispatch/pull/212" }
	}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := webhook.Event{ID: "d1", Kind: webhook.KindPullRequest, Action: "opened", Owner: "aivara-se", Repo: "dispatch", Number: 212,
		Title: "evict counts", Link: "https://github.com/aivara-se/dispatch/pull/212"}
	if event != want {
		t.Fatalf("event = %+v", event)
	}

	header.Set("X-GitHub-Event", webhook.KindPullRequestReview)
	event, err = webhook.Parse(header, []byte(`{
		"action": "submitted",
		"repository": { "full_name": "aivara-se/dispatch" },
		"pull_request": { "number": 7 },
		"review": { "state": "approved" }
	}`))
	if err != nil {
		t.Fatalf("parse review: %v", err)
	}
	if event.Number != 7 || event.Review != "approved" || event.Action != "submitted" {
		t.Fatalf("review event = %+v", event)
	}
}

func TestParseRefuses(t *testing.T) {
	body := []byte(`{"repository":{"full_name":"aivara-se/dispatch"},"pull_request":{"number":1}}`)

	header := http.Header{}
	header.Set("X-GitHub-Event", webhook.KindPullRequest)
	if _, err := webhook.Parse(header, body); !errors.Is(err, webhook.ErrNoDelivery) {
		t.Fatalf("missing id error = %v", err)
	}

	header.Set("X-GitHub-Delivery", "d1")
	header.Set("X-GitHub-Event", "issues")
	if _, err := webhook.Parse(header, body); !errors.Is(err, webhook.ErrNoPullRequest) {
		t.Fatalf("other event error = %v", err)
	}

	header.Set("X-GitHub-Event", webhook.KindPullRequest)
	if _, err := webhook.Parse(header, []byte(`{"action":"opened"}`)); !errors.Is(err, webhook.ErrNoPullRequest) {
		t.Fatalf("no pull request error = %v", err)
	}

	if _, err := webhook.Parse(header, []byte(`not json`)); err == nil {
		t.Fatal("malformed body was parsed")
	}
}

func TestHandler(t *testing.T) {
	var delivered []webhook.Event
	handler := webhook.NewHandler(secret, func(_ context.Context, event webhook.Event) error {
		delivered = append(delivered, event)
		return nil
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	body := []byte(`{
		"action": "opened",
		"repository": { "full_name": "aivara-se/dispatch" },
		"pull_request": { "number": 212 }
	}`)

	wait := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := handler.Wait(ctx); err != nil {
			t.Fatalf("wait: %v", err)
		}
	}

	post := func(signature, event string, payload []byte) *http.Response {
		request, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(string(payload)))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-GitHub-Event", event)
		request.Header.Set("X-GitHub-Delivery", "d1")
		if signature != "" {
			request.Header.Set("X-Hub-Signature-256", signature)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response
	}

	if response := post(sign(secret, body), webhook.KindPullRequest, body); response.StatusCode != http.StatusAccepted {
		t.Fatalf("valid status = %d", response.StatusCode)
	}
	wait()
	if len(delivered) != 1 || delivered[0].Number != 212 {
		t.Fatalf("delivered = %+v", delivered)
	}

	if response := post(sign([]byte("other"), body), webhook.KindPullRequest, body); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad signature status = %d", response.StatusCode)
	}
	wait()
	if len(delivered) != 1 {
		t.Fatalf("delivered after bad signature = %d", len(delivered))
	}

	other := []byte(`{"action":"opened"}`)
	if response := post(sign(secret, other), "issues", other); response.StatusCode != http.StatusAccepted {
		t.Fatalf("ignored event status = %d", response.StatusCode)
	}
	wait()
	if len(delivered) != 1 {
		t.Fatalf("delivered after ignored event = %d", len(delivered))
	}

	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d", response.StatusCode)
	}
}

func TestHandlerAnswersBeforeTheDelivery(t *testing.T) {
	// The reply must not wait on the work: a delivery that calls GitHub can
	// outlast the request GitHub waits for.
	started := make(chan struct{})
	release := make(chan struct{}, 1)
	var deadline, applied bool
	handler := webhook.NewHandler(secret, func(ctx context.Context, event webhook.Event) error {
		_, deadline = ctx.Deadline()
		close(started)
		<-release
		applied = true
		return nil
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	defer func() {
		select {
		case release <- struct{}{}:
		default:
		}
	}()

	body := []byte(`{
		"action": "opened",
		"repository": { "full_name": "aivara-se/dispatch" },
		"pull_request": { "number": 212 }
	}`)
	request, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-GitHub-Event", webhook.KindPullRequest)
	request.Header.Set("X-GitHub-Delivery", "d1")
	request.Header.Set("X-Hub-Signature-256", sign(secret, body))

	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("the reply waited for the delivery: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", response.StatusCode)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the queued delivery never ran")
	}
	if !deadline {
		t.Fatal("the delivery ran without a deadline")
	}

	release <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := handler.Wait(ctx); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !applied {
		t.Fatal("the queued delivery was not applied")
	}
}

func sign(key, body []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
