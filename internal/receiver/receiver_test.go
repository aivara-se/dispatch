package receiver

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testSecret = "route-secret"

func testAllow() Allow {
	return Allow{
		Repos:  []string{"aivara-se/dispatcher"},
		Events: []string{"issues", "pull_request"},
	}
}

func sign(secret, body string) string {
	sum := hmac.New(sha256.New, []byte(secret))
	sum.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(sum.Sum(nil))
}

func deliver(t *testing.T, h *Handler, event, id, secret, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", id)
	if secret != "" {
		req.Header.Set("X-Hub-Signature-256", sign(secret, body))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func acceptedLook(code int) func(context.Context, Delivery) int {
	return func(context.Context, Delivery) int { return code }
}

func TestBadSignatureIs401(t *testing.T) {
	h := New([]byte(testSecret), testAllow(), 0, 1<<20, acceptedLook(202))
	body := `{"action":"opened","repository":{"full_name":"aivara-se/dispatcher"},"issue":{"number":1}}`

	if rec := deliver(t, h, "issues", "delivery-1", "wrong-secret", body); rec.Code != 401 {
		t.Fatalf("wrong secret: got %d, want 401", rec.Code)
	}
	if rec := deliver(t, h, "issues", "delivery-3", "wrong-secret", "not json"); rec.Code != 401 {
		t.Fatalf("bad signature on unparsable body: got %d, want 401", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "delivery-2")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("missing signature: got %d, want 401", rec.Code)
	}
}

func TestOversizeIs413(t *testing.T) {
	h := New([]byte(testSecret), testAllow(), 0, 16, acceptedLook(202))
	body := `{"action":"opened","repository":{"full_name":"aivara-se/dispatcher"}}`
	if rec := deliver(t, h, "issues", "delivery-1", testSecret, body); rec.Code != 413 {
		t.Fatalf("got %d, want 413", rec.Code)
	}
}

func TestUnknownRepoOrEventIs202Ignored(t *testing.T) {
	h := New([]byte(testSecret), testAllow(), 0, 1<<20, acceptedLook(202))
	otherRepo := `{"action":"opened","repository":{"full_name":"aivara-se/other"},"issue":{"number":1}}`
	if rec := deliver(t, h, "issues", "delivery-1", testSecret, otherRepo); rec.Code != 202 {
		t.Fatalf("unknown repo: got %d, want 202", rec.Code)
	}
	otherEvent := `{"action":"opened","repository":{"full_name":"aivara-se/dispatcher"},"issue":{"number":1}}`
	if rec := deliver(t, h, "push", "delivery-2", testSecret, otherEvent); rec.Code != 202 {
		t.Fatalf("unknown event: got %d, want 202", rec.Code)
	}
}

func TestRepeatIDIs202Duplicate(t *testing.T) {
	calls := 0
	h := New([]byte(testSecret), testAllow(), 0, 1<<20, func(context.Context, Delivery) int {
		calls++
		return 202
	})
	body := `{"action":"opened","repository":{"full_name":"aivara-se/dispatcher"},"issue":{"number":1}}`
	if rec := deliver(t, h, "issues", "delivery-1", testSecret, body); rec.Code != 202 {
		t.Fatalf("first: got %d, want 202", rec.Code)
	}
	rec := deliver(t, h, "issues", "delivery-1", testSecret, body)
	if rec.Code != 202 {
		t.Fatalf("repeat: got %d, want 202", rec.Code)
	}
	if calls != 1 {
		t.Fatalf("look ran %d times, want 1", calls)
	}
}

func TestFailedLookIs502AndForgetsTheID(t *testing.T) {
	calls := 0
	h := New([]byte(testSecret), testAllow(), 0, 1<<20, func(context.Context, Delivery) int {
		calls++
		if calls == 1 {
			return 502
		}
		return 202
	})
	body := `{"action":"opened","repository":{"full_name":"aivara-se/dispatcher"},"issue":{"number":1}}`
	if rec := deliver(t, h, "issues", "delivery-1", testSecret, body); rec.Code != 502 {
		t.Fatalf("failed look: got %d, want 502", rec.Code)
	}
	if rec := deliver(t, h, "issues", "delivery-1", testSecret, body); rec.Code != 202 {
		t.Fatalf("redelivery after failure: got %d, want 202", rec.Code)
	}
	if calls != 2 {
		t.Fatalf("look ran %d times, want 2", calls)
	}
}

func TestActedOnDeliveryAnswersLookCode(t *testing.T) {
	var got Delivery
	h := New([]byte(testSecret), testAllow(), 0, 1<<20, func(_ context.Context, d Delivery) int {
		got = d
		return 202
	})
	body := `{"action":"opened","repository":{"full_name":"aivara-se/dispatcher"},"issue":{"number":70}}`
	if rec := deliver(t, h, "issues", "delivery-9", testSecret, body); rec.Code != 202 {
		t.Fatalf("got %d, want 202", rec.Code)
	}
	if got.ID != "delivery-9" || got.Event != "issues" || got.Repo != "aivara-se/dispatcher" || got.Action != "opened" || got.Number != 70 {
		t.Fatalf("look saw %+v", got)
	}
}

func TestBadBodyIs400(t *testing.T) {
	h := New([]byte(testSecret), testAllow(), 0, 1<<20, acceptedLook(202))
	if rec := deliver(t, h, "issues", "delivery-1", testSecret, "not json"); rec.Code != 400 {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestRateLimitIs429(t *testing.T) {
	h := New([]byte(testSecret), testAllow(), 1, 1<<20, acceptedLook(202))
	body := `{"action":"opened","repository":{"full_name":"aivara-se/dispatcher"},"issue":{"number":1}}`
	if rec := deliver(t, h, "issues", "delivery-1", testSecret, body); rec.Code != 202 {
		t.Fatalf("first: got %d, want 202", rec.Code)
	}
	if rec := deliver(t, h, "issues", "delivery-2", testSecret, body); rec.Code != 429 {
		t.Fatalf("second: got %d, want 429", rec.Code)
	}
}

func TestNonPostIs404(t *testing.T) {
	h := New([]byte(testSecret), testAllow(), 0, 1<<20, acceptedLook(202))
	req := httptest.NewRequest(http.MethodGet, "/github", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("got %d, want 404", rec.Code)
	}
}

func ExampleNew() {
	h := New([]byte(testSecret), testAllow(), 60, 1<<20, acceptedLook(http.StatusAccepted))
	fmt.Println(h != nil)
	// Output: true
}
