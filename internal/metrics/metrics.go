// Package metrics serves the service's instruments in Prometheus text on a
// second loopback listener, for a collector on the host to scrape
// ([`020`](docs/adrs/020-telemetry-leaves-through-a-collector.md),
// [`026`](docs/adrs/026-the-metrics-count-a-delivery-once.md)).
package metrics

import (
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Addr is the loopback address the instruments are scraped on. It is a
// constant and not a configuration field: it does not differ per host, and a
// collision should stop the process at boot rather than be reconfigured
// ([`020`](docs/adrs/020-telemetry-leaves-through-a-collector.md)).
const Addr = "127.0.0.1:9464"

// bounds are the histogram's bucket bounds in seconds, Prometheus' own default
// set: a delivery is decided in well under a second, and a slow one is a
// fork/exec of the child.
var bounds = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// Metrics is the three instruments, in memory and behind one lock.
type Metrics struct {
	mu         sync.Mutex
	deliveries map[series]uint64
	durations  map[string]*histogram
	followups  map[string]uint64
}

// series is one delivery counter's label set.
type series struct {
	event    string
	decision string
}

// New returns empty instruments: a scrape before any delivery names all three
// and carries no sample.
func New() *Metrics {
	return &Metrics{
		deliveries: make(map[series]uint64),
		durations:  make(map[string]*histogram),
		followups:  make(map[string]uint64),
	}
}

// Delivery counts one delivery at the decision its request produced, and times
// the handler from its entry to that decision. It is called once per audit
// decision line, so a sum over decisions is the deliveries that arrived.
func (m *Metrics) Delivery(event, decision string, took time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.deliveries[series{event: event, decision: decision}]++
	h, ok := m.durations[decision]
	if !ok {
		h = &histogram{counts: make([]uint64, len(bounds)+1)}
		m.durations[decision] = h
	}
	h.observe(took.Seconds())
}

// FollowUp counts one follow-up run at its outcome. A follow-up is not a
// delivery — no event header, no request to time — so it enters neither the
// delivery counter nor the histogram ([`026`](docs/adrs/026-the-metrics-count-a-delivery-once.md)).
func (m *Metrics) FollowUp(decision string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.followups[decision]++
}

// ServeHTTP writes every instrument as Prometheus text, version 0.0.4.
func (m *Metrics) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	m.mu.Lock()
	body := m.text()
	m.mu.Unlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Write(body)
}

// text renders the instruments, series sorted so a scrape of the same state is
// the same bytes.
func (m *Metrics) text() []byte {
	var b bytes.Buffer

	b.WriteString("# HELP dispatch_deliveries_total Deliveries answered, one per decision.\n")
	b.WriteString("# TYPE dispatch_deliveries_total counter\n")
	for _, s := range m.series() {
		fmt.Fprintf(&b, "dispatch_deliveries_total{decision=%s,event=%s} %d\n", label(s.decision), label(s.event), m.deliveries[s])
	}

	b.WriteString("# HELP dispatch_delivery_duration_seconds Time from a delivery's entry to its decision.\n")
	b.WriteString("# TYPE dispatch_delivery_duration_seconds histogram\n")
	for _, decision := range m.decisions() {
		h := m.durations[decision]
		var cumulative uint64
		for i, bound := range bounds {
			cumulative += h.counts[i]
			fmt.Fprintf(&b, "dispatch_delivery_duration_seconds_bucket{decision=%s,le=%s} %d\n",
				label(decision), label(strconv.FormatFloat(bound, 'g', -1, 64)), cumulative)
		}
		cumulative += h.counts[len(bounds)]
		fmt.Fprintf(&b, "dispatch_delivery_duration_seconds_bucket{decision=%s,le=\"+Inf\"} %d\n", label(decision), cumulative)
		fmt.Fprintf(&b, "dispatch_delivery_duration_seconds_sum{decision=%s} %s\n",
			label(decision), strconv.FormatFloat(h.sum, 'g', -1, 64))
		fmt.Fprintf(&b, "dispatch_delivery_duration_seconds_count{decision=%s} %d\n", label(decision), h.total)
	}

	b.WriteString("# HELP dispatch_followups_total Follow-up runs a held delivery's exit started.\n")
	b.WriteString("# TYPE dispatch_followups_total counter\n")
	decisions := make([]string, 0, len(m.followups))
	for decision := range m.followups {
		decisions = append(decisions, decision)
	}
	sort.Strings(decisions)
	for _, decision := range decisions {
		fmt.Fprintf(&b, "dispatch_followups_total{decision=%s} %d\n", label(decision), m.followups[decision])
	}

	return b.Bytes()
}

// series returns the delivery counter's label sets in a fixed order.
func (m *Metrics) series() []series {
	all := make([]series, 0, len(m.deliveries))
	for s := range m.deliveries {
		all = append(all, s)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].decision != all[j].decision {
			return all[i].decision < all[j].decision
		}
		return all[i].event < all[j].event
	})
	return all
}

// decisions returns the timed decisions in a fixed order.
func (m *Metrics) decisions() []string {
	all := make([]string, 0, len(m.durations))
	for decision := range m.durations {
		all = append(all, decision)
	}
	sort.Strings(all)
	return all
}

// label renders a label value as the text format requires: a backslash, a
// double quote and a newline are escaped, and nothing else is.
func label(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// histogram is a fixed-bucket duration distribution, kept as a count per bucket
// and made cumulative when it is written.
type histogram struct {
	counts []uint64
	sum    float64
	total  uint64
}

// observe records one duration, in seconds.
func (h *histogram) observe(seconds float64) {
	h.total++
	h.sum += seconds
	for i, bound := range bounds {
		if seconds <= bound {
			h.counts[i]++
			return
		}
	}
	h.counts[len(bounds)]++
}
