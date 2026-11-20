package webhook

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// The largest body GitHub posts is well under this.
const maxBody = 32 << 20

// A delivery's apply may call GitHub; this bounds one apply.
const deliveryTimeout = 60 * time.Second

// A burst larger than this is refused with 503, and GitHub retries it.
const queueSize = 256

// Handler is the HTTP entry point. It verifies the signature before it parses
// anything and answers 202 as soon as the delivery is queued. One worker
// applies the queue in arrival order, outside the request, so a delivery that
// calls GitHub never holds the reply open.
type Handler struct {
	secret  []byte
	deliver func(context.Context, Event) error

	mu    sync.Mutex
	cond  *sync.Cond
	queue []Event
	busy  bool
}

// NewHandler returns the webhook handler for one secret and one deliverer, and
// starts the worker that applies deliveries.
func NewHandler(secret []byte, deliver func(context.Context, Event) error) *Handler {
	h := &Handler{secret: secret, deliver: deliver}
	h.cond = sync.NewCond(&h.mu)
	go h.work()
	return h
}

// Wait blocks until every queued delivery has been applied, or ctx ends.
func (h *Handler) Wait(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() {
		h.mu.Lock()
		h.cond.Broadcast()
		h.mu.Unlock()
	})
	defer stop()

	h.mu.Lock()
	defer h.mu.Unlock()
	for len(h.queue) > 0 || h.busy {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		h.cond.Wait()
	}
	return nil
}

// ServeHTTP verifies, parses and queues one request.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !Verify(h.secret, body, r.Header.Get("X-Hub-Signature-256")) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	event, err := Parse(r.Header, body)
	if err != nil {
		if errors.Is(err, ErrNoPullRequest) {
			w.WriteHeader(http.StatusAccepted)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		return
	}
	if !h.enqueue(event) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// enqueue adds a delivery to the queue; a full queue reports false.
func (h *Handler) enqueue(event Event) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.queue) >= queueSize {
		return false
	}
	h.queue = append(h.queue, event)
	h.cond.Signal()
	return true
}

// work applies one delivery at a time, in arrival order.
func (h *Handler) work() {
	for {
		h.mu.Lock()
		for len(h.queue) == 0 {
			h.cond.Wait()
		}
		event := h.queue[0]
		h.queue = h.queue[1:]
		h.busy = true
		h.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), deliveryTimeout)
		err := h.deliver(ctx, event)
		cancel()
		if err != nil {
			log.Printf("dispatch: delivery %s: %v", event.ID, err)
		}

		h.mu.Lock()
		h.busy = false
		h.cond.Broadcast()
		h.mu.Unlock()
	}
}
