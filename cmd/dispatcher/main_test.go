package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aivara-se/dispatcher/internal/audit"
	"github.com/aivara-se/dispatcher/internal/board"
	"github.com/aivara-se/dispatcher/internal/config"
)

const (
	webhookSecret = "stub-webhook-secret"
	boardToken    = "stub-board-token"
	org           = "aivara-se"
	repo          = "aivara-se/dispatcher"
	foreignRepo   = "mnmtanish/dispatcher"
	unsettledCard = 9
	backlogReason = "card 9 sits in Backlog with nobody holding it; refine it"
	assigneeMama  = "thani-sh-mama"
	behindCard    = 12 // the card behind a number two cards answer
	fleetJSON     = `{"profile": "mama", "login": "thani-sh-mama"},
		{"profile": "meme", "login": "thani-sh-meme"},
		{"profile": "mimi", "login": "thani-sh-mimi"},
		{"profile": "momo", "login": "thani-sh-momo"}`
)

// auditPath puts the audit file where the test's own scratch directory holds
// it, never in the working tree. The dead-letter file sits beside it, the
// way audit.Open places it.
func auditPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "audit.jsonl")
}

// until waits, bounded, for cond: a look and a run are asynchronous, so what
// a test asserts is what they left behind, not when they left it.
func until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("waiting for %s timed out", what)
}

// =============================================================================
// The stub GitHub server: one server answering both surfaces, whose reads
// report the state its writes left behind.

// stubGitHub is one GitHub the boot reads and writes. The GraphQL endpoint
// answers the project read with the cards the stub holds at the moment of
// the call; the issue endpoint carries an assign into the next read. Every
// surface is asked for the token the configuration names, so one token
// built once at boot is part of what an integration proves.
type stubGitHub struct {
	mu    sync.Mutex
	cards map[int][]stubCard
	taken []string // the surfaces hit, in order
}

// stubCard is one card's state the stub holds: its column, its holder, and
// the repository it lives in, owner and name. A repository is part of a
// card's identity, not a second variable of it: a project can answer one
// number with two cards, an issue's number being the issue's repository's
// own — and two repositories of two owners share a name.
type stubCard struct {
	column   string
	assignee string
	repo     string
}

func newStubGitHub() *stubGitHub {
	return &stubGitHub{cards: make(map[int][]stubCard)}
}

// put seats a card on the stub's board, by number and repository.
func (s *stubGitHub) put(number int, card stubCard) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cards[number] = append(s.cards[number], card)
}

func (s *stubGitHub) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.taken...)
}

// assigns returns the issue surfaces that wrote, wherever the repository
// they wrote to lives.
func (s *stubGitHub) assigns() []string {
	var out []string
	for _, call := range s.calls() {
		if strings.HasPrefix(call, "PATCH /repos/") {
			out = append(out, call)
		}
	}
	return out
}

// start serves the stub and returns the URL --github is pointed at.
func (s *stubGitHub) start(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(server.Close)
	return server.URL
}

func (s *stubGitHub) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+boardToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	s.taken = append(s.taken, r.Method+" "+r.URL.Path)
	switch {
	case r.URL.Path == "/graphql":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		s.servePage(w)
	case strings.HasPrefix(r.URL.Path, "/repos/"):
		s.serveIssue(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// servePage answers the project read in one page: every card, with its
// repository, its column, and its first assignee. The numbers are sorted so
// a page is stable; the board's order is its own and no look reads order
// into it.
type stubNode struct {
	Content struct {
		Number     int `json:"number,omitempty"`
		Repository struct {
			Name          string `json:"name"`
			NameWithOwner string `json:"nameWithOwner"`
		} `json:"repository"`
		Assignees struct {
			Nodes []struct {
				Login string `json:"login"`
			} `json:"nodes"`
		} `json:"assignees"`
	} `json:"content"`
	Status *struct {
		Name string `json:"name"`
	} `json:"fieldValueByName"`
}

func (s *stubGitHub) servePage(w http.ResponseWriter) {
	numbers := make([]int, 0, len(s.cards))
	for number := range s.cards {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	nodes := make([]stubNode, 0, len(numbers))
	for _, number := range numbers {
		for _, card := range s.cards[number] {
			var n stubNode
			n.Content.Number = number
			n.Content.Repository.Name = card.repo[strings.LastIndex(card.repo, "/")+1:]
			n.Content.Repository.NameWithOwner = card.repo
			if card.assignee != "" {
				n.Content.Assignees.Nodes = append(n.Content.Assignees.Nodes, struct {
					Login string `json:"login"`
				}{card.assignee})
			}
			if card.column != "" {
				n.Status = &struct {
					Name string `json:"name"`
				}{card.column}
			}
			nodes = append(nodes, n)
		}
	}
	raw, err := json.Marshal(nodes)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, `{"data":{"organization":{"projectV2":{"items":{"pageInfo":{"hasNextPage":false},"nodes":%s}}}}}`, raw)
}

// serveIssue answers the REST issue endpoint, one path per repository —
// owner and name both, the way the API's path is built. An assign carries:
// the card's holder becomes the login the body names, and the next read
// reports it.
func (s *stubGitHub) serveIssue(w http.ResponseWriter, r *http.Request) {
	_, tail, hasRepo := strings.Cut(r.URL.Path, "/repos/")
	if !hasRepo {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	repoName, numberText, hasNumber := strings.Cut(tail, "/issues/")
	if !hasNumber {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	number, err := strconv.Atoi(numberText)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var card stubCard
	found := false
	for _, entered := range s.cards[number] {
		if entered.repo == repoName {
			card, found = entered, true
		}
	}
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		answerIssue(w, card)
	case http.MethodPatch:
		var body struct {
			Assignees []string `json:"assignees"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Assignees) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		card.assignee = body.Assignees[0]
		for i, entered := range s.cards[number] {
			if entered.repo == repoName {
				s.cards[number][i] = card
			}
		}
		answerIssue(w, card)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func answerIssue(w http.ResponseWriter, card stubCard) {
	type holder struct {
		Login string `json:"login"`
	}
	shown := make([]holder, 0, 1)
	if card.assignee != "" {
		shown = append(shown, holder{card.assignee})
	}
	raw, err := json.Marshal(shown)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, `{"assignees":%s}`, raw)
}

// =============================================================================
// The stub Hermes agent: an executable standing in for the child a run
// starts, recording its arguments and sleeping what STUB_SLEEP names.

// stubHermes writes the stub agent script at path.
func stubHermes(t *testing.T, path, argsLog string) {
	t.Helper()
	script := `#!/bin/sh
echo "$@" >> "` + argsLog + `"
sleep "${STUB_SLEEP:-0}"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// invocations reads the stub agent's args log, one invocation per line.
func invocations(t *testing.T, argsLog string) []string {
	t.Helper()
	raw, err := os.ReadFile(argsLog)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// =============================================================================

// configBody is a configuration JSON in the shape the fixture has: the org
// webhook as the one route, the board token named, the fleet of four. The
// address and the routes are written as given.
func configBody(addr, routes string) string {
	return fmt.Sprintf(`{
	"addr": %q,
	"path": "/github",
	"repos": ["aivara-se/dispatcher"],
	"events": ["issues", "pull_request", "projects_v2_item"],
	"owner": "aivara-se",
	"project": 2,
	"board": "DISPATCHER_BOARD_TOKEN",
	"agents": [
		%s
	],
	"routes": [%s]
}`, addr, fleetJSON, routes)
}

// theOrgRoute is the single route the fixture names.
const theOrgRoute = `{"name": "org", "secretEnv": "DISPATCHER_WEBHOOK_SECRET"}`

// freePort binds and releases one port, for a configuration whose listener a
// test then delivers to. Two tests run sequentially, so the window between
// release and boot is not contended.
func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	return addr
}

// configAt writes a configuration file with the address and routes given and
// returns its path.
func configAt(t *testing.T, addr, routes string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dispatcher.json")
	if err := os.WriteFile(path, []byte(configBody(addr, routes)), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// serveOn boots the real run on the configuration at cfgPath with a stub
// GitHub at github and the agent at hermes, both environment secrets named.
// cancel stops it; done carries the run's answer.
func serveOn(t *testing.T, cfgPath, github, hermes, auditFile string) (context.CancelFunc, chan error) {
	t.Helper()
	t.Setenv("DISPATCHER_WEBHOOK_SECRET", webhookSecret)
	t.Setenv("DISPATCHER_BOARD_TOKEN", boardToken)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{
			"--config", cfgPath,
			"--audit", auditFile,
			"--github", github,
			"--hermes", hermes,
		})
	}()
	return cancel, done
}

// deliver posts one signed delivery to the boot's listener and returns the
// answer the receiver gave. A wake carries identifiers only, and the pass
// never reads one: only the source it arrived on matters.
func deliver(t *testing.T, to, event, id, body string) int {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write([]byte(body))
	request, err := http.NewRequest(http.MethodPost, to, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Event", event)
	request.Header.Set("X-GitHub-Delivery", id)
	request.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

// wakeupBody is a delivery's payload.
const wakeupBody = `{"action":"created","number":9,"repository":{"full_name":"aivara-se/dispatcher"}}`

// theURL is where the card lives, the way the REST surface is built: the
// repository's owner and name, one path per repository.
func theURL() string {
	return fmt.Sprintf("/repos/%s/issues/%d", repo, unsettledCard)
}

// auditLines reads the audit file's lines. A file not yet written reads
// empty: a test polls for what a look writes, and a look that has written
// nothing yet is the not-now answer, not a failure.
func auditLines(t *testing.T, path string) []audit.Line {
	t.Helper()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var lines []audit.Line
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var one audit.Line
		if err := json.Unmarshal([]byte(line), &one); err != nil {
			t.Fatalf("audit line %q is not a line: %v", line, err)
		}
		lines = append(lines, one)
	}
	return lines
}

// auditHas returns the first line whose trigger matches.
func auditHas(t *testing.T, path, trigger string) (audit.Line, bool) {
	for _, line := range auditLines(t, path) {
		if line.Trigger == trigger {
			return line, true
		}
	}
	return audit.Line{}, false
}

// wokenThroughBoot waits out the whole boot look: the claim on the board,
// the run started and ended, the run-end look served — and returns the
// invocations the stub agent recorded.
func wokenThroughBoot(t *testing.T, github *stubGitHub, auditFile, argsLog string) []string {
	t.Helper()
	until(t, "the claim on the board", func() bool {
		assigns := github.assigns()
		return len(assigns) == 1 && strings.Contains(assigns[0], theURL())
	})
	until(t, "the run started", func() bool {
		return len(invocations(t, argsLog)) == 1
	})
	until(t, "the run's end served its look", func() bool {
		_, ok := auditHas(t, auditFile, "run end")
		return ok
	})
	return invocations(t, argsLog)
}

// =============================================================================
// Boot refusals: a configuration the service cannot boot with stops the
// process at boot.

func TestRunBootsWithTheFixtureConfig(t *testing.T) {
	// One unsettled card on the stub's board is what the boot look
	// reaches: the whole chain the wire-up boots answers it.
	stub := newStubGitHub()
	stub.put(unsettledCard, stubCard{column: "Backlog", repo: repo})
	github := stub.start(t)
	auditFile := auditPath(t)
	argsLog := filepath.Join(t.TempDir(), "args.log")
	hermes := filepath.Join(t.TempDir(), "hermes")
	stubHermes(t, hermes, argsLog)
	t.Setenv("STUB_SLEEP", "0.05")
	t.Setenv("DISPATCHER_WEBHOOK_SECRET", webhookSecret)
	t.Setenv("DISPATCHER_BOARD_TOKEN", boardToken)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{
			"--config", filepath.Join("testdata", "dispatcher.json"),
			"--audit", auditFile,
			"--github", github,
			"--hermes", hermes,
		})
	}()

	byLine := wokenThroughBoot(t, stub, auditFile, argsLog)
	if got := byLine[0]; !strings.Contains(got, "--profile mama chat -q") || !strings.Contains(got, backlogReason) {
		t.Errorf("the run went %q, want the fixed Backlog reason for card %d", got, unsettledCard)
	}
	// The audit's first line is the boot look's wake, with the run and
	// session ids it started.
	lines := auditLines(t, auditFile)
	if len(lines) == 0 {
		t.Fatal("the audit holds no line")
	}
	got := lines[0]
	if got.Trigger != "boot" || got.Card != unsettledCard || got.Assignee != assigneeMama ||
		got.Run == "" || got.Session == "" {
		t.Errorf("the boot's audit line is %+v, want the wake with its run and session", got)
	}
	// The GraphQL page was asked for the configured owner and project.
	// Both surfaces carry the token, which the stub verifies.
	calls := stub.calls()
	if len(calls) == 0 || !strings.Contains(calls[0], "POST /graphql") {
		t.Errorf("the boot's first surface call is %+v, want the project read", calls)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
	// The run-end look woke nobody: one run, the board unchanged.
	if got := invocations(t, argsLog); len(got) != 1 {
		t.Errorf("the run is %q, want one", got)
	}
}

func TestRunStopsAtBootWhenASecretNameResolvesToNothing(t *testing.T) {
	os.Unsetenv("DISPATCHER_WEBHOOK_SECRET")
	os.Unsetenv("DISPATCHER_BOARD_TOKEN")
	err := run(context.Background(), []string{"--config", filepath.Join("testdata", "dispatcher.json"), "--audit", auditPath(t)})
	if err == nil {
		t.Fatal("run booted with a secret name that resolves to nothing")
	}
	if !strings.Contains(err.Error(), "DISPATCHER_WEBHOOK_SECRET") {
		t.Errorf("error %q does not name the secret", err)
	}
}

func TestRunStopsAtBootWhenTheBoardTokenResolvesToNothing(t *testing.T) {
	os.Unsetenv("DISPATCHER_BOARD_TOKEN")
	t.Setenv("DISPATCHER_WEBHOOK_SECRET", webhookSecret)
	stub := newStubGitHub()
	github := stub.start(t)
	hermes := filepath.Join(t.TempDir(), "hermes")
	err := run(context.Background(), []string{
		"--config", configAt(t, freePort(t), theOrgRoute),
		"--audit", auditPath(t),
		"--github", github,
		"--hermes", hermes,
	})
	if err == nil {
		t.Fatal("run booted with the board token resolving to nothing")
	}
	if !strings.Contains(err.Error(), "DISPATCHER_BOARD_TOKEN") {
		t.Errorf("error %q does not name the board token", err)
	}
}

func TestRunRefusesAnUnknownConfigField(t *testing.T) {
	err := run(context.Background(), []string{"--config", "testdata/unknown-field.json", "--audit", auditPath(t)})
	if err == nil {
		t.Fatal("run booted on a configuration with an unknown field")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("error %q does not name the unknown field", err)
	}
}

func TestRunRefusesAnAddressAlreadyInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("DISPATCHER_WEBHOOK_SECRET", webhookSecret)
	t.Setenv("DISPATCHER_BOARD_TOKEN", boardToken)
	cfg := config.Config{
		Addr:    listener.Addr().String(),
		Path:    "/github",
		Owner:   "aivara-se",
		Project: 2,
		Board:   "DISPATCHER_BOARD_TOKEN",
		Agents:  []config.Agent{{Profile: "meme", Login: "thani-sh-meme"}},
		Routes:  []config.Route{{Name: "org", SecretEnv: "DISPATCHER_WEBHOOK_SECRET"}},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "dispatcher.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	err = run(context.Background(), []string{"--config", path, "--audit", auditPath(t)})
	if err == nil {
		t.Fatal("run booted on an address already in use")
	}
	if !strings.Contains(err.Error(), "listen") {
		t.Errorf("error %q does not name the listener", err)
	}
}

func TestRunRefusesTwoRoutes(t *testing.T) {
	t.Setenv("DISPATCHER_WEBHOOK_SECRET", webhookSecret)
	t.Setenv("DISPATCHER_OTHER_SECRET", webhookSecret)
	config := configBody(freePort(t), theOrgRoute+
		`, {"name": "second", "secretEnv": "DISPATCHER_OTHER_SECRET"}`)
	path := filepath.Join(t.TempDir(), "dispatcher.json")
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	err := run(context.Background(), []string{"--config", path, "--audit", auditPath(t)})
	if err == nil {
		t.Fatal("run booted with two routes named")
	}
	if !strings.Contains(err.Error(), "verifies exactly one") {
		t.Errorf("error %q does not name the one-route limit", err)
	}
}

// =============================================================================
// The integration: the boot against the stubs, whole.

// TestConcurrentDeliveriesDecideSerially posts two deliveries at once to a
// boot whose one unsettled card the boot look already served. The looks run
// one at a time, a ran wake for an unchanged card is not repeated, and the
// runner starts no second run for one agent: two deliveries, one run, both
// answered, each audit line carrying its own delivery.
func TestConcurrentDeliveriesDecideSerially(t *testing.T) {
	stub := newStubGitHub()
	stub.put(unsettledCard, stubCard{column: "Backlog", repo: repo})
	github := stub.start(t)
	auditFile := auditPath(t)
	argsLog := filepath.Join(t.TempDir(), "args.log")
	hermes := filepath.Join(t.TempDir(), "hermes")
	stubHermes(t, hermes, argsLog)
	t.Setenv("STUB_SLEEP", "0.05")

	addr := freePort(t)
	cancel, done := serveOn(t, configAt(t, addr, theOrgRoute), github, hermes, auditFile)
	defer cancel()

	wokenThroughBoot(t, stub, auditFile, argsLog)

	answers := make([]int, 2)
	var opened sync.WaitGroup
	opened.Add(2)
	for i := range answers {
		go func(i int) {
			defer opened.Done()
			id := fmt.Sprintf("delivery-%d", i)
			answers[i] = deliver(t, "http://"+addr+"/github", "projects_v2_item", id, wakeupBody)
		}(i)
	}
	opened.Wait()
	for i, code := range answers {
		if code != http.StatusAccepted {
			t.Errorf("delivery %d answered %d, want 202", i, code)
		}
	}
	// One run: a wake whose run went, for a card that has not changed
	// since, is not a wake to repeat.
	if got := invocations(t, argsLog); len(got) != 1 {
		t.Errorf("the deliveries started %+v, want the boot's one run only", got)
	}
	// Both looks are on the record: each with the delivery it carried.
	for i := range answers {
		id := fmt.Sprintf("delivery-%d", i)
		var found *audit.Line
		for _, line := range auditLines(t, auditFile) {
			if line.Trigger == "delivery" && len(line.Deliveries) == 1 && line.Deliveries[0] == id {
				found = &line
			}
		}
		if found == nil {
			t.Errorf("delivery %s left no audit line", id)
			continue
		}
		if found.Decision != "nothing to wake" || found.Answer != http.StatusAccepted {
			t.Errorf("delivery %s's line reads %+v, want the suppressed look it would not repeat", id, found)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestTheDroppedWakeIsRepairedAtTheNextLook boots with a Hermes path that
// holds nothing: the boot look claims the card, fails the start, and
// dead-letters the miss. Writing the agent script is the fix. The next
// look — a delivery's — repairs the wake: the same claim is not written
// twice, and the run the assignment promised starts.
func TestTheDroppedWakeIsRepairedAtTheNextLook(t *testing.T) {
	stub := newStubGitHub()
	stub.put(unsettledCard, stubCard{column: "Backlog", repo: repo})
	github := stub.start(t)
	auditFile := auditPath(t)
	dir := t.TempDir()
	argsLog := filepath.Join(dir, "args.log")
	hermes := filepath.Join(dir, "hermes") // written into after the boot look fails
	t.Setenv("STUB_SLEEP", "0.05")

	addr := freePort(t)
	cancel, done := serveOn(t, configAt(t, addr, theOrgRoute), github, hermes, auditFile)
	defer cancel()

	until(t, "the claim the run could not start", func() bool {
		return len(stub.assigns()) == 1 && strings.Contains(stub.assigns()[0], theURL())
	})
	deadFile := filepath.Join(filepath.Dir(auditFile), "audit.dead.jsonl")
	until(t, "the miss on the dead-letter file", func() bool {
		_, err := os.Stat(deadFile)
		return err == nil
	})
	raw, err := os.ReadFile(deadFile)
	if err != nil {
		t.Fatal(err)
	}
	var miss audit.Miss
	if err := json.Unmarshal(raw, &miss); err != nil {
		t.Fatalf("the dead letter is not a miss: %v", err)
	}
	if !strings.Contains(miss.Reason, "start the run for mama") {
		t.Errorf("the miss %q does not name the failed run", miss.Reason)
	}

	stubHermes(t, hermes, argsLog)
	answer := deliver(t, "http://"+addr+"/github", "projects_v2_item", "repair-1", wakeupBody)
	if answer != http.StatusAccepted {
		t.Errorf("the repair delivery answered %d, want 202", answer)
	}
	until(t, "the repaired run started", func() bool {
		return len(invocations(t, argsLog)) == 1
	})
	// The repair repeats no claim: the board already carries it.
	if got := stub.assigns(); len(got) != 1 {
		t.Errorf("the repair assigned %+v, want the boot's one claim only", got)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestTheConfiguredPathServesTheReceiverAndNothingElse boots on an empty
// board and asks the listener what the mux must answer: a GET on the
// configured path and a GET on a path the chart does not name are both 404,
// and the boot look's line is on the record.
func TestTheConfiguredPathServesTheReceiverAndNothingElse(t *testing.T) {
	stub := newStubGitHub()
	github := stub.start(t)
	auditFile := auditPath(t)
	argsLog := filepath.Join(t.TempDir(), "args.log")
	hermes := filepath.Join(t.TempDir(), "hermes")
	stubHermes(t, hermes, argsLog)
	t.Setenv("STUB_SLEEP", "0")

	addr := freePort(t)
	cancel, done := serveOn(t, configAt(t, addr, theOrgRoute), github, hermes, auditFile)
	defer cancel()

	// The empty board is an answer, not a failure: the boot look reads
	// it and decides nothing to wake.
	until(t, "the boot look's line", func() bool {
		line, ok := auditHas(t, auditFile, "boot")
		return ok && line.Decision == "nothing to wake"
	})
	for _, probe := range []struct {
		path string
	}{
		{"/github"},
		{"/other"},
	} {
		response, err := http.Get("http://" + addr + probe.path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("a GET on %s answered %d, want 404", probe.path, response.StatusCode)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestTwoCardsWithOneNumberRefuseTheWrite boots a board where two repositories
// answer one card number — an issue number is the issue's repository's own,
// and one project can hold both. Here the two repositories share a name and
// differ only in their owner, which a name alone cannot tell apart; the
// assign refuses the write instead of guessing the repository the board did
// not name for the decision, and the miss is dead-lettered with both
// repositories: the next look repeats, and under no claim does a run start
// whose assignee never went.
func TestTwoCardsWithOneNumberRefuseTheWrite(t *testing.T) {
	stub := newStubGitHub()
	stub.put(unsettledCard, stubCard{column: "Backlog", repo: repo})
	stub.put(unsettledCard, stubCard{column: "Backlog", repo: foreignRepo})
	github := stub.start(t)
	auditFile := auditPath(t)
	argsLog := filepath.Join(t.TempDir(), "args.log")
	hermes := filepath.Join(t.TempDir(), "hermes")
	stubHermes(t, hermes, argsLog)
	t.Setenv("STUB_SLEEP", "0")

	addr := freePort(t)
	cancel, done := serveOn(t, configAt(t, addr, theOrgRoute), github, hermes, auditFile)
	defer cancel()

	deadFile := filepath.Join(filepath.Dir(auditFile), "audit.dead.jsonl")
	until(t, "the refused assign", func() bool {
		_, err := os.Stat(deadFile)
		return err == nil
	})
	raw, err := os.ReadFile(deadFile)
	if err != nil {
		t.Fatal(err)
	}
	var miss audit.Miss
	if err := json.Unmarshal(raw, &miss); err != nil {
		t.Fatalf("the dead letter is not a miss: %v", err)
	}
	if !strings.Contains(miss.Reason, "two cards") || !strings.Contains(miss.Reason, repo) || !strings.Contains(miss.Reason, foreignRepo) {
		t.Errorf("the miss %q does not name the two repositories the number answers", miss.Reason)
	}
	// No assign reached the stub, so no issue holds a claim the pass wrote
	// for a number it could not resolve.
	if got := stub.assigns(); len(got) != 0 {
		t.Errorf("the look wrote %+v, want nothing it could not resolve", got)
	}
	// And no run is started under it.
	if got := invocations(t, argsLog); len(got) != 0 {
		t.Errorf("the run table holds %+v, want one run the write was refused of", got)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestAClaimThatCannotLandStopsNoCardBehindIt boots a board whose number 9 is
// answered by two repositories and whose card 12 is not. The collision's claim
// can never land, and the look walks past it: card 12 is claimed and its run
// starts in that same look, while the refusal is dead-lettered with both
// repositories named. A card the pass cannot claim costs itself, never the
// cards behind it.
func TestAClaimThatCannotLandStopsNoCardBehindIt(t *testing.T) {
	stub := newStubGitHub()
	stub.put(unsettledCard, stubCard{column: "Backlog", repo: repo})
	stub.put(unsettledCard, stubCard{column: "Backlog", repo: foreignRepo})
	stub.put(behindCard, stubCard{column: "Backlog", repo: repo})
	github := stub.start(t)
	auditFile := auditPath(t)
	argsLog := filepath.Join(t.TempDir(), "args.log")
	hermes := filepath.Join(t.TempDir(), "hermes")
	stubHermes(t, hermes, argsLog)
	t.Setenv("STUB_SLEEP", "0")

	addr := freePort(t)
	cancel, done := serveOn(t, configAt(t, addr, theOrgRoute), github, hermes, auditFile)
	defer cancel()

	behind := fmt.Sprintf("/repos/%s/issues/%d", repo, behindCard)
	until(t, "the claim on the card behind the collision", func() bool {
		for _, call := range stub.assigns() {
			if strings.HasSuffix(call, behind) {
				return true
			}
		}
		return false
	})
	if got := stub.assigns(); len(got) != 1 {
		t.Errorf("the boot wrote %+v, want one claim: the card behind the collision", got)
	}
	until(t, "the run for the card behind the collision", func() bool {
		return len(invocations(t, argsLog)) == 1
	})
	if got := invocations(t, argsLog); !strings.Contains(got[0], fmt.Sprintf("card %d sits in Backlog", behindCard)) {
		t.Errorf("the run went %q, want the reason for card %d", got[0], behindCard)
	}
	// The boot's line names the card it woke, not the one it refused.
	if line, ok := auditHas(t, auditFile, "boot"); !ok || line.Card != behindCard || line.Assignee != assigneeMama {
		t.Errorf("the boot line read as %+v, want the wake for card %d", line, behindCard)
	}
	// The refusal is on the record, with both repositories it could not
	// choose between. Every later look refuses it again, so the file's
	// first line is the boot look's.
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(auditFile), "audit.dead.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var miss audit.Miss
	if err := json.Unmarshal([]byte(strings.Split(strings.TrimSpace(string(raw)), "\n")[0]), &miss); err != nil {
		t.Fatalf("the dead letter is not a miss: %v", err)
	}
	if !strings.Contains(miss.Reason, fmt.Sprintf("two cards on the project answer %d", unsettledCard)) ||
		!strings.Contains(miss.Reason, repo) || !strings.Contains(miss.Reason, foreignRepo) {
		t.Errorf("the miss %q does not name the collision it refused", miss.Reason)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestTheWriteRefusesWhatTheReadCannotPlace pins the two refusals the writer
// makes before it builds a URL: a number no read placed, and a repository
// that is not an owner and a name. Neither refusal reaches GitHub — a write
// that went out on either would land wherever the URL happened to point.
func TestTheWriteRefusesWhatTheReadCannotPlace(t *testing.T) {
	cases := []struct {
		name string
		repo string
		want string
	}{
		{"a number no read placed", "", "no read placed card"},
		{"a repository that is not an owner and a name", "dispatcher", "is not a repository's owner and name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := newStubGitHub()
			github := stub.start(t)
			theBoard := &bridge{
				client: board.NewAt(boardToken, github+"/graphql", github),
				owner:  org,
				repos:  make(map[int]string),
			}
			if c.repo != "" {
				theBoard.repos[unsettledCard] = c.repo
			}
			err := theBoard.Assign(context.Background(), unsettledCard, assigneeMama)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("assign answered %v, want %q", err, c.want)
			}
			if got := stub.calls(); len(got) != 0 {
				t.Errorf("a refused write reached GitHub: %v", got)
			}
		})
	}
}

// TestACardOfAnotherOwnerIsClaimedWhereTheBoardNamesIt boots a board whose
// one card lives in a repository the configured owner does not own, under a
// name the configured owner has too. The page names a card's repository as
// its owner and name, and the claim is written at that repository's issue:
// a card is written where the board named it, never at the configured owner
// with a name two owners share.
func TestACardOfAnotherOwnerIsClaimedWhereTheBoardNamesIt(t *testing.T) {
	stub := newStubGitHub()
	stub.put(unsettledCard, stubCard{column: "Backlog", repo: foreignRepo})
	github := stub.start(t)
	auditFile := auditPath(t)
	argsLog := filepath.Join(t.TempDir(), "args.log")
	hermes := filepath.Join(t.TempDir(), "hermes")
	stubHermes(t, hermes, argsLog)
	t.Setenv("STUB_SLEEP", "0")

	addr := freePort(t)
	cancel, done := serveOn(t, configAt(t, addr, theOrgRoute), github, hermes, auditFile)
	defer cancel()

	until(t, "the claim the boot look writes", func() bool {
		return len(stub.assigns()) == 1
	})
	if got := stub.assigns(); !strings.Contains(got[0], "/repos/"+foreignRepo+"/issues/9") {
		t.Fatalf("the look wrote %+v, want one claim at %s", got, foreignRepo)
	}
	until(t, "the run the claim started", func() bool {
		return len(invocations(t, argsLog)) == 1
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
}
