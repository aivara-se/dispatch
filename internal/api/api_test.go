package api_test

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aivara-se/dispatch/internal/api"
	"github.com/aivara-se/dispatch/internal/record"
	_ "modernc.org/sqlite"
)

func TestRoutes(t *testing.T) {
	store, handler, _, dbPath := newServer(t)
	job, _, err := store.EnsureJob(context.Background(), record.Start{
		Flow: "review", Owner: "aivara-se", Repo: "dispatch", Number: 212, Key: "212",
		Title: "evict counts drift under load",
		Link:  "https://github.com/aivara-se/dispatch/pull/212",
	})
	if err != nil {
		t.Fatal(err)
	}
	seedTask(t, dbPath, job.ID)

	health := get(t, handler, "/v1/health")
	if health.Code != http.StatusOK {
		t.Fatalf("health = %d", health.Code)
	}

	flows := decode(t, get(t, handler, "/v1/flows"))
	list := flows["flows"].([]any)
	if len(list) != 2 {
		t.Fatalf("flows = %v", list)
	}
	counts := map[string]float64{}
	for _, item := range list {
		flow := item.(map[string]any)
		counts[flow["name"].(string)] = flow["jobs"].(float64)
	}
	if counts["review"] != 1 || counts["audit"] != 0 {
		t.Fatalf("counts = %v", counts)
	}

	jobs := decode(t, get(t, handler, "/v1/flows/review/jobs"))
	cards := jobs["jobs"].([]any)
	if len(cards) != 1 {
		t.Fatalf("jobs = %v", cards)
	}
	card := cards[0].(map[string]any)
	if card["ref"] != "aivara-se/dispatch#212" || card["key"] != "212" || card["state"] != "started" || card["verdict"] != "pending" {
		t.Fatalf("card = %v", card)
	}
	if _, ok := card["alert"]; !ok || card["alert"] != nil {
		t.Fatalf("alert = %v", card["alert"])
	}
	if _, ok := card["tasks"]; ok {
		t.Fatal("a card carries tasks")
	}
	if _, err := time.Parse(time.RFC3339, card["cause"].(string)); err != nil {
		t.Fatalf("cause = %v", card["cause"])
	}

	detail := decode(t, get(t, handler, "/v1/flows/review/jobs/aivara-se/dispatch/212"))
	tasks := detail["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %v", tasks)
	}
	task := tasks[0].(map[string]any)
	if task["n"] != float64(1) || task["short"] != "review" || task["at"] != "2026-11-19T00:00:00Z" {
		t.Fatalf("task = %v", task)
	}
	if _, ok := task["pc"].(map[string]any); !ok {
		t.Fatalf("pc = %v", task["pc"])
	}
	if _, ok := task["evidence"].(map[string]any); !ok {
		t.Fatalf("evidence = %v", task["evidence"])
	}
	if _, ok := task["tools"].([]any); !ok {
		t.Fatalf("tools = %v", task["tools"])
	}
	actions := task["actions"].([]any)
	if len(actions) != 3 {
		t.Fatalf("actions = %v", actions)
	}
	action := actions[0].(map[string]any)
	if action["type"] != "prompt" || action["agent"] != "momo" || action["text"] != "Review it." || action["output"] != "done" || action["exit"] != float64(0) {
		t.Fatalf("action = %v", action)
	}
	if _, ok := action["path"]; ok {
		t.Fatal("a prompt carries path")
	}
	if _, ok := action["name"]; ok {
		t.Fatal("a prompt carries name")
	}
	script := actions[1].(map[string]any)
	if script["type"] != "script" || script["path"] != "scripts/check.sh" || script["args"].([]any)[0] != "--fast" {
		t.Fatalf("script = %v", script)
	}
	if _, ok := script["agent"]; ok {
		t.Fatal("a script carries agent")
	}
	label := actions[2].(map[string]any)
	if label["type"] != "action" || label["name"] != "label" || label["value"] != "blocked" {
		t.Fatalf("label = %v", label)
	}
	if _, ok := label["path"]; ok {
		t.Fatal("an action carries path")
	}

	if code := get(t, handler, "/v1/flows/missing/jobs").Code; code != http.StatusNotFound {
		t.Fatalf("missing flow = %d", code)
	}
	if code := get(t, handler, "/v1/flows/review/jobs/aivara-se/dispatch/999").Code; code != http.StatusNotFound {
		t.Fatalf("missing job = %d", code)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/flows", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d", recorder.Code)
	}
}

func TestUnknownFlowIsNotFoundEvenWithAJob(t *testing.T) {
	store, handler, _, _ := newServer(t)
	if _, _, err := store.EnsureJob(context.Background(), record.Start{
		Flow: "hidden", Owner: "aivara-se", Repo: "dispatch", Number: 1, Key: "1",
	}); err != nil {
		t.Fatal(err)
	}
	if code := get(t, handler, "/v1/flows/hidden/jobs").Code; code != http.StatusNotFound {
		t.Fatalf("hidden flow list = %d", code)
	}
	if code := get(t, handler, "/v1/flows/hidden/jobs/aivara-se/dispatch/1").Code; code != http.StatusNotFound {
		t.Fatalf("hidden flow job = %d", code)
	}
}

func TestStream(t *testing.T) {
	store, handler, hub, _ := newServer(t)
	_, _, err := store.EnsureJob(context.Background(), record.Start{
		Flow: "review", Owner: "aivara-se", Repo: "dispatch", Number: 212, Key: "212",
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Timeout: 3 * time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)

	first := readEvent(t, reader)
	if first.Flow != "review" || first.Key != "212" {
		t.Fatalf("snapshot = %+v", first)
	}

	hub.Publish("review", "aivara-se", "dispatch", "213")
	second := readEvent(t, reader)
	if second.Key != "213" {
		t.Fatalf("live event = %+v", second)
	}

	cancel()
	response.Body.Close()

	// A new connection starts with the current state.
	if _, _, err := store.EnsureJob(context.Background(), record.Start{
		Flow: "audit", Owner: "aivara-se", Repo: "dispatch", Number: 300, Key: "300",
	}); err != nil {
		t.Fatal(err)
	}
	response2, err := client.Get(server.URL + "/v1/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer response2.Body.Close()
	reconnected := readEvent(t, bufio.NewReader(response2.Body))
	if reconnected.Flow != "audit" || reconnected.Key != "300" {
		t.Fatalf("reconnect = %+v", reconnected)
	}
}

func newServer(t *testing.T) (*record.Store, http.Handler, *api.Hub, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "dispatch.db")
	store, err := record.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	hub := api.NewHub()
	return store, api.New(store, []string{"review", "audit"}, hub).Handler(), hub, dbPath
}

func seedTask(t *testing.T, dbPath string, jobID int64) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	result, err := db.Exec(
		`INSERT INTO tasks (job_id, name, n, state, pc, evidence, tools, started_at)
		 VALUES (?, 'review', 1, 'done', ?, ?, ?, '2026-11-19T00:00:00Z')`,
		jobID,
		`{"expr":"pr.created","want":true,"got":true}`,
		`{"head":"abc"}`,
		`[{"name":"read"}]`)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(
		`INSERT INTO actions (task_id, ordinal, type, agent, text, output, exit)
		 VALUES (?, 0, 'prompt', 'momo', 'Review it.', 'done', 0)`, taskID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(
		`INSERT INTO actions (task_id, ordinal, type, path, args, output, exit)
		 VALUES (?, 1, 'script', 'scripts/check.sh', '["--fast"]', 'ok', 0)`, taskID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(
		`INSERT INTO actions (task_id, ordinal, type, name, value, output, exit)
		 VALUES (?, 2, 'action', 'label', 'blocked', '', 0)`, taskID)
	if err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decode(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return body
}

func readEvent(t *testing.T, reader *bufio.Reader) api.Event {
	t.Helper()
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read event: %v", err)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event api.Event
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &event); err != nil {
			t.Fatalf("event %q: %v", line, err)
		}
		return event
	}
}
