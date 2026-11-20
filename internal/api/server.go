// Package api serves the record read-only over a Unix socket: flows, jobs,
// one job and the change stream.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/aivara-se/dispatch/internal/record"
)

// Server serves the record over HTTP; the hub is its only mutable state.
type Server struct {
	store  *record.Store
	flows  []string
	served map[string]bool
	hub    *Hub
}

// New returns the read-only HTTP handler for the configured flows.
func New(store *record.Store, flows []string, hub *Hub) *Server {
	names := append([]string(nil), flows...)
	sort.Strings(names)
	served := make(map[string]bool, len(names))
	for _, name := range names {
		served[name] = true
	}
	return &Server{store: store, flows: names, served: served, hub: hub}
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
	if !s.served[flow] {
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
	if !s.served[flow] {
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

// stream sends every job's current state, then each change as it lands. A
// client that falls behind refetches on its next event.
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
		if !s.served[job.Flow] {
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
