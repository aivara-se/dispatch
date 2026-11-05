package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// scrape serves the instruments and returns the text a collector would read.
func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	return w.Body.String()
}

func want(t *testing.T, text string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains(text, line) {
			t.Errorf("the exposition lacks %q:\n%s", line, text)
		}
	}
}

func TestScrapeNamesTheThreeInstruments(t *testing.T) {
	w := httptest.NewRecorder()
	New().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got, want := w.Header().Get("Content-Type"), "text/plain; version=0.0.4; charset=utf-8"; got != want {
		t.Fatalf("content type = %q, want %q", got, want)
	}
	want(t, w.Body.String(),
		"# TYPE dispatch_deliveries_total counter",
		"# TYPE dispatch_delivery_duration_seconds histogram",
		"# TYPE dispatch_followups_total counter",
	)
}

func TestADeliveryCountsOnceAtItsDecision(t *testing.T) {
	m := New()
	m.Delivery("pull_request", "started", 3*time.Millisecond)
	m.Delivery("pull_request", "held", 2*time.Millisecond)
	m.Delivery("pull_request", "held", 2*time.Millisecond)

	text := scrape(t, m)
	want(t, text,
		`dispatch_deliveries_total{decision="started",event="pull_request"} 1`,
		`dispatch_deliveries_total{decision="held",event="pull_request"} 2`,
		`dispatch_delivery_duration_seconds_count{decision="started"} 1`,
		`dispatch_delivery_duration_seconds_count{decision="held"} 2`,
		`dispatch_delivery_duration_seconds_sum{decision="started"} 0.003`,
		`dispatch_delivery_duration_seconds_bucket{decision="started",le="0.005"} 1`,
		`dispatch_delivery_duration_seconds_bucket{decision="started",le="+Inf"} 1`,
	)
}

func TestTheEventIsTheHeaderVerbatim(t *testing.T) {
	m := New()
	for _, decision := range []string{"started", "held", "ignored", "duplicate", "start_failed"} {
		m.Delivery("workflow_run", decision, time.Millisecond)
	}

	text := scrape(t, m)
	for _, decision := range []string{"started", "held", "ignored", "duplicate", "start_failed"} {
		want(t, text, `dispatch_deliveries_total{decision="`+decision+`",event="workflow_run"} 1`)
	}
}

func TestAFollowUpIsItsOwnCounter(t *testing.T) {
	m := New()
	m.Delivery("pull_request", "held", time.Millisecond)
	m.FollowUp("started")
	m.FollowUp("start_failed")

	text := scrape(t, m)
	want(t, text,
		`dispatch_followups_total{decision="started"} 1`,
		`dispatch_followups_total{decision="start_failed"} 1`,
	)
	if strings.Contains(text, `dispatch_deliveries_total{decision="started"`) {
		t.Errorf("a follow-up entered the delivery counter:\n%s", text)
	}
	if strings.Contains(text, `dispatch_delivery_duration_seconds_count{decision="started"}`) {
		t.Errorf("a follow-up was timed:\n%s", text)
	}
}

func TestALabelValueIsEscaped(t *testing.T) {
	m := New()
	m.Delivery("odd\"event\\name\n", "ignored", time.Millisecond)

	want(t, scrape(t, m), `dispatch_deliveries_total{decision="ignored",event="odd\"event\\name\n"} 1`)
}

func TestOnlyAGetIsServed(t *testing.T) {
	w := httptest.NewRecorder()
	New().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}
