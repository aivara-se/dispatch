package router

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aivara-se/dispatcher/internal/audit"
	"github.com/aivara-se/dispatcher/internal/board"
	"github.com/aivara-se/dispatcher/internal/receiver"
)

var fleet = map[string]string{
	"thani-sh-mama": "mama",
	"thani-sh-meme": "meme",
	"thani-sh-mimi": "mimi",
}

// lookedAt is the one moment the pass's clock returns, so every reason the
// tests see is an exact string compared by equality, not by containment.
var lookedAt = time.Date(2026, 10, 16, 6, 46, 33, 0, time.UTC)

// ops records the pass's actions in the order it took them, so a test can
// read a look: the cards claimed, the runs asked for, and whether the write
// came before the run it prepares.
type ops struct {
	mu   sync.Mutex
	list []string
}

func (o *ops) take(what string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.list = append(o.list, what)
}

func (o *ops) count(what string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := 0
	for _, got := range o.list {
		if strings.HasPrefix(got, what) {
			n++
		}
	}
	return n
}

// stubBoard is the board the pass reads and writes, keeping its own state
// true: an assign the pass made is on the cards it reads back, the way the
// durable marker is on the real board.
type stubBoard struct {
	ops          *ops
	mu           sync.Mutex
	cards        []board.Card
	failReads    int
	readAttempts int
	failWrites   int
	refuses      map[int]bool // the numbers the board refuses to write: two cards answer them
	assigns      []string
}

func (b *stubBoard) ReadAll(ctx context.Context) ([]board.Card, error) {
	b.mu.Lock()
	b.readAttempts++
	fail := b.failReads > 0
	if fail {
		b.failReads--
	}
	cards := make([]board.Card, len(b.cards))
	copy(cards, b.cards)
	b.mu.Unlock()
	if fail {
		return nil, errors.New("graphql answered 500")
	}
	return cards, nil
}

func (b *stubBoard) Assign(ctx context.Context, number int, login string) error {
	b.ops.take(fmt.Sprintf("assign %d %s", number, login))
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.refuses[number] {
		return fmt.Errorf("card %d is answered by two cards, so the repository to write is a guess", number)
	}
	if b.failWrites > 0 {
		b.failWrites--
		return errors.New("the endpoint answered 500")
	}
	b.assigns = append(b.assigns, fmt.Sprintf("%d %s", number, login))
	for i, card := range b.cards {
		if card.Number == number {
			b.cards[i].Assignee = login
		}
	}
	return nil
}

type stubRun struct {
	profile string
	reason  string
}

// stubRunner is one run per profile, remembered the way the runner's table
// is: Running reads the map a start set.
type stubRunner struct {
	ops        *ops
	mu         sync.Mutex
	running    map[string]bool
	started    []stubRun
	failStarts int
}

func (s *stubRunner) Start(ctx context.Context, profile, reason string) (string, string, error) {
	s.ops.take(fmt.Sprintf("start %s", profile))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failStarts > 0 {
		s.failStarts--
		return "", "", errors.New("the child would not start")
	}
	s.running[profile] = true
	s.started = append(s.started, stubRun{profile: profile, reason: reason})
	return profile + "-run-1", profile + "-session-1", nil
}

func (s *stubRunner) Running(profile string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running[profile]
}

type stubLog struct {
	mu     sync.Mutex
	lines  []audit.Line
	misses []audit.Miss
}

func (s *stubLog) Append(line audit.Line) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, line)
	return nil
}

func (s *stubLog) Dead(miss audit.Miss) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.misses = append(s.misses, miss)
	return nil
}

// boot is one pass over a board of cards: the parts built the way the
// binary will build them, on a board that keeps itself true, and the clock
// fixed so the reasons are compared by equality. The package's fleet serves
// the rule, so a refusal here is a bug in this file and not a case under
// test: boot fails rather than answer a router it does not have.
func boot(t *testing.T, cards []board.Card) (*Router, *stubBoard, *stubRunner, *stubLog, *ops) {
	t.Helper()
	track := &ops{}
	boardy := &stubBoard{ops: track, cards: cards}
	runner := &stubRunner{ops: track, running: map[string]bool{}}
	stub := &stubLog{}
	router, err := New(boardy, boardy, runner, stub, fleet)
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	router.now = func() time.Time { return lookedAt }
	return router, boardy, runner, stub, track
}

func delivery(id string) receiver.Delivery {
	return receiver.Delivery{ID: id, Event: "issues", Repo: "aivara-se/dispatcher", Action: "opened"}
}

// TestAFleetThatCannotServeTheRuleIsRefused pins the check the boot makes on
// its own fleet: the rule names one refiner and the builders, and a fleet
// without either role would boot, read the board and wake nobody for the
// cards that role serves. The refusal names the profile the rule needs and
// the fleet lacks.
func TestAFleetThatCannotServeTheRuleIsRefused(t *testing.T) {
	cases := []struct {
		name    string
		fleet   map[string]string
		missing string
	}{
		{
			"builders and no refiner",
			map[string]string{"thani-sh-meme": "meme", "thani-sh-mimi": "mimi"},
			"mama",
		},
		{
			"no builder",
			map[string]string{"thani-sh-mama": "mama", "thani-sh-momo": "momo"},
			"builder",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			boardy := &stubBoard{ops: &ops{}}
			router, err := New(boardy, boardy, &stubRunner{ops: &ops{}, running: map[string]bool{}}, &stubLog{}, c.fleet)
			if err == nil {
				t.Fatal("New built a router for a fleet the rule cannot be served by")
			}
			if router != nil {
				t.Errorf("New answered a router beside its refusal")
			}
			if !strings.Contains(err.Error(), c.missing) {
				t.Errorf("the refusal %q does not name %q", err, c.missing)
			}
		})
	}
}

// TestAFleetWithTheRefinerAndOneBuilderServes pins the other half of the
// judgement: the rule needs a builder, not every builder, so a fleet of the
// refiner and one developer is served.
func TestAFleetWithTheRefinerAndOneBuilderServes(t *testing.T) {
	boardy := &stubBoard{ops: &ops{}}
	router, err := New(boardy, boardy, &stubRunner{ops: &ops{}, running: map[string]bool{}}, &stubLog{}, map[string]string{
		"thani-sh-mama": "mama",
		"thani-sh-meme": "meme",
	})
	if err != nil {
		t.Fatalf("New refused a fleet that serves the rule: %v", err)
	}
	if router == nil {
		t.Fatal("New answered no router for a fleet that serves the rule")
	}
}

func TestABacklogCardWakesItsRefinerOncePerLook(t *testing.T) {
	router, _, runner, stub, track := boot(t, []board.Card{
		{Number: 5, Column: "Backlog"},
		{Number: 8, Column: "Backlog"},
	})
	router.OnBoot(context.Background())

	if !reflect.DeepEqual(track.list, []string{
		"assign 5 thani-sh-mama", // the write, before the run it prepares
		"start mama",             // one run per agent per look, the lowest-numbered card
	}) {
		t.Fatalf("the look took %v", track.list)
	}
	if got, want := runner.started[0].reason, reasonBacklog(5, lookedAt); got != want {
		t.Errorf("the reason was %q, want %q", got, want)
	}
	wantLine := audit.Line{
		Trigger:  "boot",
		Decision: "woke mama for card 5",
		Card:     5,
		Assignee: "thani-sh-mama",
		Run:      "mama-run-1",
		Session:  "mama-session-1",
	}
	if got := stub.lines; !reflect.DeepEqual(got, []audit.Line{wantLine}) {
		t.Errorf("the log read as %+v, want %+v", got, []audit.Line{wantLine})
	}
	if len(stub.misses) != 0 {
		t.Errorf("the look dead-lettered %+v", stub.misses)
	}
}

func TestADeliveryIsAnsweredOnTheLook(t *testing.T) {
	router, _, runner, stub, _ := boot(t, []board.Card{{Number: 72, Column: "Refined"}})
	code := router.OnDelivery(context.Background(), delivery("d-72"))
	if code != http.StatusAccepted {
		t.Fatalf("delivered %d, want 202", code)
	}
	wantLine := audit.Line{
		Trigger:    "delivery",
		Deliveries: []string{"d-72"},
		Decision:   "woke meme for card 72",
		Card:       72,
		Assignee:   "thani-sh-meme",
		Run:        "meme-run-1",
		Session:    "meme-session-1",
		Answer:     http.StatusAccepted,
	}
	if got := stub.lines; !reflect.DeepEqual(got, []audit.Line{wantLine}) {
		t.Errorf("the log read as %+v, want %+v", got, []audit.Line{wantLine})
	}
	if len(runner.started) != 1 || runner.started[0].reason != reasonRefined(72, lookedAt) {
		t.Errorf("the look started %+v, want one run with the refined card's reason", runner.started)
	}
}

func TestRefinedCardsGoToAFreeDeveloperEach(t *testing.T) {
	router, board, runner, _, _ := boot(t, []board.Card{
		{Number: 80, Column: "Refined"},
		{Number: 81, Column: "Refined"},
	})
	router.OnBoot(context.Background())

	if got := board.assigns; !reflect.DeepEqual(got, []string{"80 thani-sh-meme", "81 thani-sh-mimi"}) {
		t.Errorf("the claims were %v, want the free developers one card each", got)
	}
	if len(runner.started) != 2 {
		t.Errorf("the pass started %d runs, want 2", len(runner.started))
	}
}

func TestAFinishedCardIsNoDevelopersHold(t *testing.T) {
	router, board, runner, _, _ := boot(t, []board.Card{
		{Number: 69, Column: "Finished", Assignee: "thani-sh-meme"},
		{Number: 70, Column: "Finished", Assignee: "thani-sh-mimi"},
		{Number: 71, Column: "Finished", Assignee: "thani-sh-meme"},
		{Number: 100, Column: "Refined"},
	})
	router.OnBoot(context.Background())
	if got := board.assigns; !reflect.DeepEqual(got, []string{"100 thani-sh-meme"}) {
		t.Errorf("the claims were %v, want meme's claim on card 100", got)
	}
	if len(runner.started) != 1 || runner.started[0].profile != "meme" {
		t.Errorf("the pass started %+v, want meme on card 100", runner.started)
	}
}

func TestARefinedCardWaitsWhenNoDeveloperIsFree(t *testing.T) {
	router, board, runner, stub, _ := boot(t, []board.Card{
		{Number: 80, Column: "Refined"},
		{Number: 81, Column: "Started", Assignee: "thani-sh-meme"},
		{Number: 82, Column: "Started", Assignee: "thani-sh-mimi"},
	})
	runner.running["meme"] = true
	runner.running["mimi"] = true
	router.OnBoot(context.Background())
	if len(runner.started) != 0 {
		t.Errorf("the pass started %+v, want no run while no developer is free", runner.started)
	}
	if len(board.assigns) != 0 {
		t.Errorf("the pass wrote %+v, want the card to wait", board.assigns)
	}
	if got := stub.lines; len(got) != 1 || got[0].Decision != "nothing to wake" {
		t.Errorf("the log read as %+v, want one nothing-to-wake line", got)
	}
}

func TestARunEndHandsTheFreeDeveloperTheNextCard(t *testing.T) {
	router, _, runner, _, _ := boot(t, []board.Card{
		{Number: 90, Column: "Refined"},
		{Number: 91, Column: "Started", Assignee: "thani-sh-mimi"},
	})
	runner.running["mimi"] = true // her run is in flight; her card waits with her
	router.OnBoot(context.Background())
	if len(runner.started) != 1 || runner.started[0].profile != "meme" {
		t.Fatalf("the first look started %+v, want meme for card 90", runner.started)
	}

	relook, board, runner, _, _ := boot(t, []board.Card{
		{Number: 90, Column: "Refined", Assignee: "thani-sh-meme"},
		{Number: 91, Column: "Refined"}, // mimi's hold ended with her run
	})
	relook.OnRunEnd(context.Background(), "mimi")
	if got := board.assigns; !reflect.DeepEqual(got, []string{"91 thani-sh-mimi"}) {
		t.Errorf("the run-end look wrote %v, want only card 91's claim", got)
	}
	if len(runner.started) != 1 || runner.started[0].profile != "mimi" {
		t.Errorf("the run-end look started %+v, want mimi on card 91", runner.started)
	}
}

func TestAStartedCardWaitsWhileItsRunIsInFlight(t *testing.T) {
	router, _, runner, stub, _ := boot(t, []board.Card{{Number: 9, Column: "Started", Assignee: "thani-sh-meme"}})
	runner.running["meme"] = true
	router.OnBoot(context.Background())
	if len(runner.started) != 0 {
		t.Errorf("the pass started %+v for an agent with a run in flight", runner.started)
	}
	if got := stub.lines; len(got) != 1 || got[0].Decision != "nothing to wake" {
		t.Errorf("the log read as %+v, want one nothing-to-wake line", got)
	}
}

func TestAStartedCardIsWokenExactlyOnceForItsState(t *testing.T) {
	router, _, runner, stub, _ := boot(t, []board.Card{{Number: 9, Column: "Started", Assignee: "thani-sh-meme"}})
	if code := router.OnDelivery(context.Background(), delivery("d-0")); code != http.StatusAccepted {
		t.Fatalf("first look answered %d, want 202", code)
	}
	if len(runner.started) != 1 {
		t.Fatalf("first look started %+v, want one run", runner.started)
	}
	code := router.OnDelivery(context.Background(), delivery("d-9"))
	if code != http.StatusAccepted {
		t.Fatalf("second look answered %d, want 202", code)
	}
	if len(runner.started) != 1 {
		t.Errorf("the pass started a second run for the agent's one state, want suppression")
	}
	wantLine := audit.Line{
		Trigger:    "delivery",
		Deliveries: []string{"d-9"},
		Decision:   "nothing to wake",
		Answer:     http.StatusAccepted,
	}
	if got := stub.lines[1]; !reflect.DeepEqual(got, wantLine) {
		t.Errorf("second look read as %+v, want %+v", got, wantLine)
	}
}

func TestAStartedCardAtBootStartsTheRunItsClaimPromises(t *testing.T) {
	router, _, _, _, _ := boot(t, []board.Card{{Number: 9, Column: "Started", Assignee: "thani-sh-meme"}})
	router.OnBoot(context.Background()) // the run went; the agent left the card where it was

	reboot, _, runner, _, _ := boot(t, []board.Card{{Number: 9, Column: "Started", Assignee: "thani-sh-meme"}})
	reboot.OnBoot(context.Background())
	if len(runner.started) != 1 || runner.started[0].reason != reasonStarted(9, lookedAt) {
		t.Errorf("the boot look started %+v, want its agent restarted for the unkept promise", runner.started)
	}
}

func TestARestartRepeatsNoClaimTheBoardCarries(t *testing.T) {
	router, _, _, _, _ := boot(t, []board.Card{{Number: 72, Column: "Refined"}})
	router.OnBoot(context.Background()) // meme is claimed, and the claim is on the board

	reboot, board, runner, stub, _ := boot(t, []board.Card{{Number: 72, Column: "Refined", Assignee: "thani-sh-meme"}})
	reboot.OnBoot(context.Background())
	if len(board.assigns) != 0 {
		t.Errorf("the boot look rewrote %+v, want the claim the board already carries", board.assigns)
	}
	if len(runner.started) != 0 {
		t.Errorf("the boot look started %+v, want nobody for a claimed card", runner.started)
	}
	if got := stub.lines; len(got) != 1 || got[0].Decision != "nothing to wake" {
		t.Errorf("the log read as %+v, want one nothing-to-wake line", got)
	}
}

func TestADroppedWakeIsRepairedAtTheNextLook(t *testing.T) {
	router, _, runner, stub, track := boot(t, []board.Card{{Number: 72, Column: "Refined"}})
	runner.failStarts = attempts // every try fails, so the first look drops its wake

	code := router.OnDelivery(context.Background(), delivery("d-72"))
	if code != http.StatusBadGateway {
		t.Fatalf("the dropped wake answered %d, want 502", code)
	}
	if got := track.count("assign 72"); got != 1 {
		t.Errorf("the write happened %d times, want once", got)
	}
	want := audit.Miss{
		Reason: "start the run for meme: the child would not start",
		Line: audit.Line{
			Trigger:    "delivery",
			Deliveries: []string{"d-72"},
			Decision:   "refused",
			Card:       72,
			Assignee:   "thani-sh-meme",
			Answer:     http.StatusBadGateway,
		},
	}
	if got := stub.misses; len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Errorf("the miss read as %+v, want %+v", got, []audit.Miss{want})
	}

	code = router.OnDelivery(context.Background(), delivery("d-73"))
	if code != http.StatusAccepted {
		t.Fatalf("the repaired wake answered %d, want 202", code)
	}
	if got := track.count("assign 72"); got != 1 {
		t.Errorf("the repair rewrote the claim: %d writes", got)
	}
	if len(runner.started) != 1 || runner.started[0].reason != reasonRefined(72, lookedAt) {
		t.Errorf("the look started %+v, want the run that repairs the card", runner.started)
	}
	if len(stub.misses) != 1 {
		t.Errorf("the second look dead-lettered %+v, want the first look's only", stub.misses)
	}
}

func TestAFailedWriteIsRetriedThenRefused(t *testing.T) {
	router, board, _, stub, _ := boot(t, []board.Card{{Number: 72, Column: "Refined"}})
	board.failWrites = attempts // both tries fail

	code := router.OnDelivery(context.Background(), delivery("d-72"))
	if code != http.StatusBadGateway {
		t.Fatalf("the failed write answered %d, want 502", code)
	}
	if got := len(stub.misses); got != 1 {
		t.Fatalf("the look wrote %d misses, want one", got)
	}
	if got := stub.misses[0]; !strings.HasPrefix(got.Reason, "assign card 72:") {
		t.Errorf("the miss said %q, want the failed assign named", got.Reason)
	}
	code = router.OnDelivery(context.Background(), delivery("d-73"))
	if code != http.StatusAccepted {
		t.Fatalf("the retry answered %d, want 202", code)
	}
}

// TestARefusedClaimHandsTheSlotToTheNextCard pins what a permanent refusal and
// one wake per agent per look have to keep together: the card whose claim the
// board refused stays unclaimed and is dead-lettered, and the agent's one wake
// goes to the next card naming it, in that same look — so a card the pass can
// never claim stops no card behind it.
func TestARefusedClaimHandsTheSlotToTheNextCard(t *testing.T) {
	router, board, runner, stub, track := boot(t, []board.Card{
		{Number: 9, Column: "Backlog"},
		{Number: 12, Column: "Backlog"},
	})
	board.refuses = map[int]bool{9: true}

	code := router.OnDelivery(context.Background(), delivery("d-9"))
	if code != http.StatusBadGateway {
		t.Errorf("a look whose claim was refused answered %d, want 502", code)
	}
	want := []string{
		"assign 9 thani-sh-mama", // the refused claim: one try and its retry
		"assign 9 thani-sh-mama",
		"assign 12 thani-sh-mama", // the same look's next card naming the agent
		"start mama",
	}
	if got := track.list; !reflect.DeepEqual(got, want) {
		t.Errorf("the look took %v, want %v", got, want)
	}
	if got := board.assigns; !reflect.DeepEqual(got, []string{"12 thani-sh-mama"}) {
		t.Errorf("the look wrote %v, want the card behind the refusal claimed", got)
	}
	if len(runner.started) != 1 || runner.started[0].reason != reasonBacklog(12, lookedAt) {
		t.Errorf("the look started %+v, want one run, concerning card 12", runner.started)
	}
	if len(stub.misses) != 1 {
		t.Fatalf("the look dead-lettered %+v, want one miss: the refused claim", stub.misses)
	}
	if got := stub.misses[0]; got.Line.Card != 9 || !strings.HasPrefix(got.Reason, "assign card 9:") {
		t.Errorf("the miss read as %+v, want card 9's refused claim", got)
	}
	wantLine := audit.Line{
		Trigger:    "delivery",
		Deliveries: []string{"d-9"},
		Decision:   "woke mama for card 12",
		Card:       12,
		Assignee:   "thani-sh-mama",
		Run:        "mama-run-1",
		Session:    "mama-session-1",
		Answer:     http.StatusBadGateway, // the refusal refuses the look back
	}
	if got := stub.lines; !reflect.DeepEqual(got, []audit.Line{wantLine}) {
		t.Errorf("the log read as %+v, want %+v", got, []audit.Line{wantLine})
	}
}

// TestARefusedClaimSpendsOneWakePerAgentPerLook pins the other half: a look
// that has already spent an agent's wake starts no second run for it, and the
// card behind the refusal is reached by the next look instead — a refusal
// defers a card, it never stops the board.
func TestARefusedClaimSpendsOneWakePerAgentPerLook(t *testing.T) {
	router, first, runner, _, _ := boot(t, []board.Card{
		{Number: 5, Column: "Backlog"},
		{Number: 9, Column: "Backlog"},
		{Number: 12, Column: "Backlog"},
	})
	first.refuses = map[int]bool{9: true}

	router.OnBoot(context.Background())
	if got := first.assigns; !reflect.DeepEqual(got, []string{"5 thani-sh-mama"}) {
		t.Errorf("the look wrote %v, want one claim: the lowest-numbered card", got)
	}
	if len(runner.started) != 1 || runner.started[0].reason != reasonBacklog(5, lookedAt) {
		t.Fatalf("the look started %+v, want one run, concerning card 5", runner.started)
	}

	// The next look, the board as that one left it: card 5 carries its
	// claim, card 9's can never land, and the wake goes to card 12.
	relook, next, nextRunner, _, _ := boot(t, []board.Card{
		{Number: 5, Column: "Backlog", Assignee: "thani-sh-mama"},
		{Number: 9, Column: "Backlog"},
		{Number: 12, Column: "Backlog"},
	})
	next.refuses = map[int]bool{9: true}
	relook.OnBoot(context.Background())
	if got := next.assigns; !reflect.DeepEqual(got, []string{"12 thani-sh-mama"}) {
		t.Errorf("the next look wrote %v, want the claim on card 12", got)
	}
	if len(nextRunner.started) != 1 || nextRunner.started[0].reason != reasonBacklog(12, lookedAt) {
		t.Errorf("the next look started %+v, want one run, concerning card 12", nextRunner.started)
	}
}

// TestAFailedBoardReadRefusesTheLook pins the refusal on both records: the
// audit log's line for the look, and the miss beside it. A look whose read
// failed is past verification, so it is logged (SYSTEMS.md, step 9; ADR
// 006) as well as dead-lettered.
func TestAFailedBoardReadRefusesTheLook(t *testing.T) {
	router, board, _, stub, _ := boot(t, []board.Card{{Number: 72, Column: "Refined"}})
	board.failReads = attempts // both tries fail

	code := router.OnDelivery(context.Background(), delivery("d-72"))
	if code != http.StatusBadGateway {
		t.Fatalf("a failed read answered %d, want 502", code)
	}
	if got := board.readAttempts; got != attempts {
		t.Errorf("the read happened %d times, want %d", got, attempts)
	}
	wantLine := audit.Line{
		Trigger:    "delivery",
		Deliveries: []string{"d-72"},
		Decision:   "refused",
		Answer:     http.StatusBadGateway,
	}
	if got := stub.lines; !reflect.DeepEqual(got, []audit.Line{wantLine}) {
		t.Errorf("the log read as %+v, want the look's line %+v", got, wantLine)
	}
	want := audit.Miss{
		Reason: "read the board: graphql answered 500",
		Line:   wantLine,
	}
	if got := stub.misses; len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Errorf("the miss read as %+v, want %+v", got, []audit.Miss{want})
	}
}

// TestAFailedBootReadWritesBothRecords is the same refusal with no
// delivery behind it: the boot look that cannot read the board appends its
// line, with no answer to carry, and dead-letters the reason beside it.
func TestAFailedBootReadWritesBothRecords(t *testing.T) {
	router, board, _, stub, _ := boot(t, []board.Card{{Number: 72, Column: "Refined"}})
	board.failReads = attempts // both tries fail

	router.OnBoot(context.Background())

	wantLine := audit.Line{Trigger: "boot", Decision: "refused"}
	if got := stub.lines; !reflect.DeepEqual(got, []audit.Line{wantLine}) {
		t.Errorf("the log read as %+v, want the look's line %+v", got, wantLine)
	}
	wantMiss := audit.Miss{Reason: "read the board: graphql answered 500", Line: wantLine}
	if got := stub.misses; len(got) != 1 || !reflect.DeepEqual(got[0], wantMiss) {
		t.Errorf("the miss read as %+v, want %+v", got, []audit.Miss{wantMiss})
	}
}

func TestABadStartRetriesTheRun(t *testing.T) {
	router, _, runner, stub, _ := boot(t, []board.Card{{Number: 72, Column: "Refined"}})
	runner.failStarts = 1 // one transient failure, one try left in the look

	code := router.OnDelivery(context.Background(), delivery("d-72"))
	if code != http.StatusAccepted {
		t.Fatalf("a retried start answered %d, want 202", code)
	}
	if len(runner.started) != 1 || len(stub.misses) != 0 {
		t.Errorf("the look left %+v started and %d misses, want one start and no misses", runner.started, len(stub.misses))
	}
}

func TestCardsTheRuleDoesNotAnswerWait(t *testing.T) {
	router, _, runner, stub, _ := boot(t, []board.Card{
		{Number: 5, Column: "Finished", Assignee: "thani-sh-meme"},
		{Number: 6, Column: ""},
		{Number: 7, Column: "Blocked"},
		{Number: 8, Column: "Backlog", Assignee: "thani-sh-mama"},
		{Number: 9, Column: "Started"},                        // the rule names what moves it, when its state names nobody
		{Number: 10, Column: "Started", Assignee: "thani-sh"}, // the operator is never woken
	})
	router.OnBoot(context.Background())
	if len(runner.started) != 0 {
		t.Errorf("the pass started %+v for cards the rule does not answer", runner.started)
	}
	if got := stub.lines; len(got) != 1 || got[0].Decision != "nothing to wake" {
		t.Errorf("the log read as %+v, want one nothing-to-wake line", got)
	}
}

func TestNoReasonCarriesThePayload(t *testing.T) {
	router, _, runner, _, _ := boot(t, []board.Card{
		{Number: 5, Column: "Backlog"},
		{Number: 7, Column: "Refined"},
		{Number: 9, Column: "Started", Assignee: "thani-sh-meme"},
	})
	sentinel := delivery("third-party-sentinel")
	sentinel.Event = "third-party-sentinel"
	sentinel.Repo = "third-party-sentinel/aivara"
	sentinel.Action = "third-party-sentinel"
	router.OnDelivery(context.Background(), sentinel)

	for _, run := range runner.started {
		if strings.Contains(run.reason, "third-party-sentinel") {
			t.Errorf("the reason carried the payload: %q", run.reason)
		}
	}
	want := map[string]string{
		"mama": reasonBacklog(5, lookedAt),
		"meme": reasonStarted(9, lookedAt),
		"mimi": reasonRefined(7, lookedAt),
	}
	if len(runner.started) != len(want) {
		t.Fatalf("the pass woke %+v, want the three rules the board calls for", runner.started)
	}
	for _, run := range runner.started {
		if got, ok := want[run.profile]; !ok || run.reason != got {
			t.Errorf("%s's reason was %q, want %q", run.profile, run.reason, got)
		}
	}
}

func TestTwoLookAtOnceDecideInTurn(t *testing.T) {
	reader := &serialBoard{cards: []board.Card{{Number: 13, Column: "Refined"}}}
	router, err := New(reader, reader, &stubRunner{ops: &ops{}, running: map[string]bool{}}, &stubLog{}, fleet)
	if err != nil {
		t.Fatalf("the package fleet does not serve the rule: %v", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var codes []int
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code := router.OnDelivery(context.Background(), delivery(fmt.Sprintf("d-%d", i)))
			mu.Lock()
			codes = append(codes, code)
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if most := reader.most.Load(); most != 1 {
		t.Errorf("the reader saw %d passes over it at once, want 1", most)
	}
	for _, code := range codes {
		if code != http.StatusAccepted {
			t.Errorf("a concurrent look answered %d, want 202", code)
		}
	}
}

// serialBoard is a reader that counts how many reads it serves at once, so
// a test can see whether two looks decide against the same records.
type serialBoard struct {
	cards []board.Card
	live  atomic.Int64
	most  atomic.Int64
}

func (s *serialBoard) ReadAll(ctx context.Context) ([]board.Card, error) {
	now := s.live.Add(1)
	for {
		most := s.most.Load()
		if now <= most || s.most.CompareAndSwap(most, now) {
			break
		}
	}
	time.Sleep(20 * time.Millisecond)
	s.live.Add(-1)
	return s.cards, nil
}

func (s *serialBoard) Assign(ctx context.Context, number int, login string) error {
	return nil
}
