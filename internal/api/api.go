// Package api serves the record read-only over a Unix socket: flows, jobs,
// one job and the change stream.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/aivara-se/dispatch/internal/record"
)

// Event names a job that changed.
type Event struct {
	Flow  string `json:"flow"`
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Key   string `json:"key"`
}

// Hub fans a change out to every connected stream.
type Hub struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{subs: map[chan Event]struct{}{}}
}

// Publish sends one change to every subscriber. A slow subscriber drops it;
// the next job fetch or a reconnect carries the current state.
func (h *Hub) Publish(flow, owner, repo, key string) {
	event := Event{Flow: flow, Owner: owner, Repo: repo, Key: key}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (h *Hub) subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 16)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

// Server serves the record. It holds no state of its own beyond the hub.
type Server struct {
	store *record.Store
	flows []string
	hub   *Hub
}

// New returns the read-only HTTP handler for the configured flows.
func New(store *record.Store, flows []string, hub *Hub) *Server {
	names := append([]string(nil), flows...)
	sort.Strings(names)
	return &Server{store: store, flows: names, hub: hub}
}

// Handler returns the routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.health)
	mux.HandleFunc("GET /v1/flows", s.flowsList)
	mux.HandleFunc("GET /v1/flows/{flow}/jobs", s.jobs)
	mux.HandleFunc("GET /v1/flows/{flow}/jobs/{owner}/{repo}/{key}", s.job)
	mux.HandleFunc("GET /v1/stream", s.stream)
	return mux
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) flowsList(w http.ResponseWriter, r *http.Request) {
	counts, err := s.store.FlowCounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError)
		return
	}
	type flow struct {
		Name string `json:"name"`
		Jobs int    `json:"jobs"`
	}
	flows := make([]flow, 0, len(s.flows))
	for _, name := range s.flows {
		flows = append(flows, flow{Name: name, Jobs: counts[name]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"flows": flows})
}

func (s *Server) jobs(w http.ResponseWriter, r *http.Request) {
	flow := r.PathValue("flow")
	if !s.known(flow) {
		writeError(w, http.StatusNotFound)
		return
	}
	jobs, err := s.store.Jobs(r.Context(), flow)
	if err != nil {
		writeError(w, http.StatusInternalServerError)
		return
	}
	cards := make([]jobCard, 0, len(jobs))
	for _, job := range jobs {
		cards = append(cards, cardOf(job))
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": cards})
}

func (s *Server) job(w http.ResponseWriter, r *http.Request) {
	flow := r.PathValue("flow")
	if !s.known(flow) {
		writeError(w, http.StatusNotFound)
		return
	}
	job, err := s.store.JobByKey(r.Context(), flow,
		r.PathValue("owner"), r.PathValue("repo"), r.PathValue("key"))
	if err == record.ErrNotFound {
		writeError(w, http.StatusNotFound)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError)
		return
	}
	tasks, err := s.store.Tasks(r.Context(), job.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError)
		return
	}
	view := jobDetail{jobCard: cardOf(*job), Tasks: make([]taskView, 0, len(tasks))}
	for _, task := range tasks {
		view.Tasks = append(view.Tasks, taskOf(task))
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError)
		return
	}
	events, cancel := s.hub.subscribe()
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	jobs, err := s.store.AllJobs(r.Context())
	if err != nil {
		return
	}
	for _, job := range jobs {
		if !s.known(job.Flow) {
			continue
		}
		sendEvent(w, flusher, Event{Flow: job.Flow, Owner: job.Owner, Repo: job.Repo, Key: job.Key})
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case event := <-events:
			sendEvent(w, flusher, event)
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func sendEvent(w http.ResponseWriter, flusher http.Flusher, event Event) {
	body, err := json.Marshal(event)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", body)
	flusher.Flush()
}

func (s *Server) known(flow string) bool {
	for _, name := range s.flows {
		if name == flow {
			return true
		}
	}
	return false
}

type jobCard struct {
	Flow    string  `json:"flow"`
	Owner   string  `json:"owner"`
	Repo    string  `json:"repo"`
	Key     string  `json:"key"`
	Ref     string  `json:"ref"`
	Title   string  `json:"title"`
	State   string  `json:"state"`
	Verdict string  `json:"verdict"`
	Cause   string  `json:"cause"`
	Alert   *string `json:"alert"`
	Link    string  `json:"link"`
}

type jobDetail struct {
	jobCard
	Tasks []taskView `json:"tasks"`
}

type taskView struct {
	N        int             `json:"n"`
	Short    string          `json:"short"`
	At       string          `json:"at"`
	Actions  []actionView    `json:"actions"`
	PC       json.RawMessage `json:"pc"`
	Evidence json.RawMessage `json:"evidence"`
	Tools    json.RawMessage `json:"tools"`
}

type actionView struct {
	Type    string   `json:"type"`
	Output  *string  `json:"output"`
	Exit    *int     `json:"exit"`
	Skipped bool     `json:"skipped,omitempty"`
	Agent   string   `json:"agent,omitempty"`
	Text    string   `json:"text,omitempty"`
	Path    string   `json:"path,omitempty"`
	Args    []string `json:"args,omitempty"`
	Name    string   `json:"name,omitempty"`
	Value   string   `json:"value,omitempty"`
}

func cardOf(job record.Job) jobCard {
	card := jobCard{
		Flow:    job.Flow,
		Owner:   job.Owner,
		Repo:    job.Repo,
		Key:     job.Key,
		Ref:     fmt.Sprintf("%s/%s#%d", job.Owner, job.Repo, job.Number),
		Title:   job.Title,
		State:   job.State,
		Verdict: job.Verdict,
		Cause:   job.UpdatedAt.Format(time.RFC3339),
		Link:    job.Link,
	}
	if job.Alert != "" {
		alert := job.Alert
		card.Alert = &alert
	}
	return card
}

func taskOf(task record.Task) taskView {
	view := taskView{
		N:        task.N,
		Short:    task.Short,
		At:       task.At,
		Actions:  make([]actionView, 0, len(task.Actions)),
		PC:       raw(task.PC),
		Evidence: raw(task.Evidence),
		Tools:    raw(task.Tools),
	}
	for _, action := range task.Actions {
		item := actionView{Type: action.Type, Exit: action.Exit, Skipped: action.Skipped}
		if action.Output != "" {
			output := action.Output
			item.Output = &output
		}
		switch action.Type {
		case "prompt":
			item.Agent = action.Agent
			item.Text = action.Text
		case "script":
			item.Path = action.Path
			item.Args = action.Args
		case "action":
			item.Name = action.Name
			item.Value = action.Value
		}
		view.Actions = append(view.Actions, item)
	}
	return view
}

func raw(text string) json.RawMessage {
	if text == "" || !json.Valid([]byte(text)) {
		return nil
	}
	return json.RawMessage(text)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int) {
	message := "not found"
	if status != http.StatusNotFound {
		message = "internal error"
	}
	writeJSON(w, status, map[string]string{"error": message})
}
