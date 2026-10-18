// Package receiver verifies GitHub deliveries, drops what is not ours,
// deduplicates redeliveries, and answers every request.
//
// The handler reads the raw body up to its limit, verifies
// X-Hub-Signature-256 before parsing anything, refuses what fails, answers
// 202 for what verifies but needs no action, and otherwise waits for the
// look function — the router's decision — and answers what it returns. A
// look that reports a server failure fails the delivery, and the delivery
// id is forgotten so a redelivery after the fix is not swallowed as a
// duplicate.
package receiver

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// dedupTTL is how long a delivery id is remembered. It matches the
// gateway's own adapter, so the two hops forget at the same rate.
const dedupTTL = time.Hour

// rateWindow is the fixed window the per-minute inbound limit counts in.
const rateWindow = time.Minute

// Allow is the allowlist a delivery must match to be acted on. Anything
// else verifies and is then answered 202 and dropped: a 4xx is for a
// request that was wrong, not for one that is merely ours to ignore.
type Allow struct {
	Repos  []string
	Events []string
}

// Delivery is what a verified request carried: the delivery id and event
// from the headers, and the repository, action and card number from the
// body. Number is zero where the payload named none.
type Delivery struct {
	ID     string
	Event  string
	Repo   string
	Action string
	Number int
}

// Handler answers GitHub deliveries. It is built once per route with that
// route's secret: a secret belongs to a source, never to the service.
type Handler struct {
	secret    []byte
	repos     map[string]bool
	events    map[string]bool
	perMinute int
	bodyLimit int64
	look      func(context.Context, Delivery) int

	mu          sync.Mutex
	seen        map[string]time.Time
	windowStart time.Time
	windowCount int
}

// New builds a Handler. secret is the route's webhook secret, allow the
// repositories and events to act on, perMinute the inbound limit per
// minute (zero or negative disables it), bodyLimit the largest body read
// before refusal, and look the router's decision, waited for on every
// delivery that needs one. look answers the HTTP status for the delivery;
// a 5xx from it fails the delivery.
func New(secret []byte, allow Allow, perMinute int, bodyLimit int64, look func(context.Context, Delivery) int) *Handler {
	return &Handler{
		secret:    append([]byte(nil), secret...),
		repos:     set(allow.Repos),
		events:    set(allow.Events),
		perMinute: perMinute,
		bodyLimit: bodyLimit,
		look:      look,
		seen:      make(map[string]time.Time),
	}
}

// ServeHTTP answers one delivery. It waits for the look, which is what
// makes a failed board call a 502.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !h.withinLimit(time.Now()) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, h.bodyLimit+1))
	if err != nil {
		http.Error(w, "cannot read body", http.StatusBadRequest)
		return
	}
	if int64(len(body)) > h.bodyLimit {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if !h.verified(body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}

	delivery, err := parse(r.Header.Get("X-GitHub-Delivery"), r.Header.Get("X-GitHub-Event"), body)
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if !h.repos[delivery.Repo] || !h.events[delivery.Event] {
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintln(w, "ignored")
		return
	}
	if delivery.ID != "" && !h.remember(delivery.ID, time.Now()) {
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintln(w, "duplicate")
		return
	}

	code := h.look(r.Context(), delivery)
	if code < 100 || code > 599 {
		code = http.StatusBadGateway
	}
	if code >= 500 && delivery.ID != "" {
		h.forget(delivery.ID)
	}
	w.WriteHeader(code)
}

// verified reports whether the signature header matches the HMAC-SHA256 of
// the raw body. The comparison is constant time; nothing is parsed before
// it passes.
func (h *Handler) verified(body []byte, header string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	sum := hmac.New(sha256.New, h.secret)
	sum.Write(body)
	return subtle.ConstantTimeCompare(sum.Sum(nil), sig) == 1
}

// withinLimit reports whether the request fits the per-minute budget. The
// limit is counted before anything is parsed and before anything is
// logged; it bounds what an unauthenticated POST can cost. It counts per
// handler, not per source: per-source buckets need their own reason in the
// change that adds them.
func (h *Handler) withinLimit(now time.Time) bool {
	if h.perMinute <= 0 {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if now.Sub(h.windowStart) >= rateWindow {
		h.windowStart = now
		h.windowCount = 0
	}
	if h.windowCount >= h.perMinute {
		return false
	}
	h.windowCount++
	return true
}

// remember records the delivery id and reports whether it is new. A repeat
// within the hour is a redelivery of the same delivery, answered without
// starting anything.
func (h *Handler) remember(id string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweep(now)
	if _, ok := h.seen[id]; ok {
		return false
	}
	h.seen[id] = now
	return true
}

// forget drops the delivery id, so a delivery the look failed — refused,
// never silently lost — is acted on when it is sent again rather than
// swallowed as a duplicate.
func (h *Handler) forget(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.seen, id)
}

// sweep drops expired entries. It runs on every insert, which is what
// bounds the cache: it never holds more than an hour of deliveries.
func (h *Handler) sweep(now time.Time) {
	for id, at := range h.seen {
		if now.Sub(at) >= dedupTTL {
			delete(h.seen, id)
		}
	}
}

// parse builds the delivery from the headers and the verified body. Every
// field it uses is read as data: no field becomes a path, a command, a
// URL or a query, and no body text is logged.
func parse(id, event string, body []byte) (Delivery, error) {
	var payload struct {
		Action     string `json:"action"`
		Number     int    `json:"number"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Issue struct {
			Number int `json:"number"`
		} `json:"issue"`
		PullRequest struct {
			Number int `json:"number"`
		} `json:"pull_request"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&payload); err != nil {
		return Delivery{}, fmt.Errorf("decode payload: %w", err)
	}
	number := payload.Number
	if number == 0 {
		number = payload.Issue.Number
	}
	if number == 0 {
		number = payload.PullRequest.Number
	}
	return Delivery{
		ID:     id,
		Event:  event,
		Repo:   payload.Repository.FullName,
		Action: payload.Action,
		Number: number,
	}, nil
}

func set(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}
