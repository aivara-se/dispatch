// Package router runs the state pass: it reads the whole board, asks every
// card the rule PRODUCT.md fixes — what moves it forward, who does it, and
// is it already happening — and starts the runs the rule calls for, one per
// agent per look, at the lowest-numbered card that names it and whose claim
// lands, the number first and the repository's name after it.
//
// A card below the board read is its repository and its number: an issue's
// number is the issue's repository's own, so one project answers one number
// with more than one card, and the pair is what every record and every write
// the pass names.
//
// It looks in three places and nowhere else: a delivery arrives, a run ends,
// and once at boot. The looks run one at a time behind one mutex, so two
// deliveries never decide against the same records. A look repeats no wake:
// it remembers the action it took for a card, in memory, and the durable
// half of that memory is the board itself — the assignee the pass wrote —
// which is what makes a restart reconcile the board instead of repeating
// it.
package router

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aivara-se/dispatcher/internal/audit"
	"github.com/aivara-se/dispatcher/internal/board"
	"github.com/aivara-se/dispatcher/internal/receiver"
)

// Reader is the read half of the board the pass decides from: the whole
// project read, every card with its column and its assignee. The board's
// order is its own; the claim sorts what the rule names by the pair.
type Reader interface {
	ReadAll(ctx context.Context) ([]board.Card, error)
}

// Writer is the write half: the one field the service writes anywhere, the
// assignee of a card. The card is named below the read, by its repository
// and its number: the repository is the one the board named for the card the
// rule decided on, so a number two cards answer is written at the right one
// of them. A write happens before the run it prepares, so the board shows
// the claim even when the run fails to start.
type Writer interface {
	Assign(ctx context.Context, repo string, number int, login string) error
}

// Runner is the run table the pass asks whether a run is in flight, and
// where it starts one. Start answers the run and session ids, so the audit
// line records what the look started; Running is the fact the rule's
// "is it already happening" turns on.
type Runner interface {
	Start(ctx context.Context, profile, reason string) (run, session string, err error)
	Running(profile string) bool
}

// Logger is the service's record of itself: the audit line appended per
// look, and the miss written when an action the look tried fails.
type Logger interface {
	Append(audit.Line) error
	Dead(audit.Miss) error
}

// identity is a card below the board read: the repository the board named and
// the issue's number, the pair that tells two cards of one project apart. The
// pass keys every per-card record by it, orders the rule's names by it, and
// writes it into every line it leaves behind.
type identity struct {
	repo   string
	number int
}

// ident is the identity of one card the read gave the pass.
func ident(card board.Card) identity {
	return identity{repo: card.Repo, number: card.Number}
}

// String is how one card's identity reads in the service's own words: the
// repository the board named, then the issue's number.
func (c identity) String() string {
	return fmt.Sprintf("%s#%d", c.repo, c.number)
}

// Router runs the passes. It holds no state of the board's: the board read
// is the decision's input and the assignee write is its record, and what
// lives between looks is only which action was attempted and whether it
// ran.
type Router struct {
	reader Reader
	writer Writer
	runner Runner
	log    Logger
	login  map[string]string // GitHub login to the Hermes profile the run uses

	mu        sync.Mutex
	attempted map[identity]attempt
	now       func() time.Time
}

// New builds the router. login maps each agent's GitHub login to the Hermes
// profile their run uses, the way the configuration names them: the logins
// are the configurable half of the fleet, the profiles are the rule's. A
// fleet the rule cannot be served by is refused here, where the boot can
// still stop, and the error names the profile the rule needs and the fleet
// lacks.
func New(reader Reader, writer Writer, runner Runner, log Logger, login map[string]string) (*Router, error) {
	if err := servesRule(login); err != nil {
		return nil, err
	}
	return &Router{
		reader:    reader,
		writer:    writer,
		runner:    runner,
		log:       log,
		login:     login,
		attempted: make(map[identity]attempt),
		now:       time.Now,
	}, nil
}

// attempt is the pass's memory of the action it took for a card: the board
// state it acted on — the column it read and the assignee it left on the
// card — and whether that action's wake started a run. The board's assignee
// field is the durable half of this record: after a restart the pass sees
// the claim it wrote there, and what is lost is only whether the run went.
type attempt struct {
	column   string
	assignee string
	ran      bool
}

// OnDelivery answers a delivery with a look and returns the HTTP status the
// receiver sends back. The delivery is the trigger and the audit line's id,
// and nothing more: the pass asks the board and the run table, never the
// payload, so nothing a third party wrote reaches a reason (ADR 009).
func (r *Router) OnDelivery(ctx context.Context, delivery receiver.Delivery) int {
	return r.look(ctx, "delivery", []string{delivery.ID})
}

// OnRunEnd answers the end of one agent's run with a look: the cards that
// waited on that run are asked the rule again, with no delivery behind
// them. profile is the agent whose run ended; the look serves the whole
// board, and its agent's next work lands with it like any other look's.
func (r *Router) OnRunEnd(ctx context.Context, profile string) {
	r.look(ctx, "run end", nil)
}

// OnBoot answers boot with a look: every card the board holds is asked the
// rule with no delivery behind it, which is what reaches a card that
// generates no event.
func (r *Router) OnBoot(ctx context.Context) {
	r.look(ctx, "boot", nil)
}

// The per-card rule's agents, as PRODUCT.md fixes them: MaMa refines a
// Backlog card, MeMe and MiMi build what sits in Refined. Their logins come
// from the login map, which is all a configuration changes.
const refinerProfile = "mama"

var builderProfiles = []string{"meme", "mimi"}

// servesRule refuses a fleet the rule cannot be served by. The rule names one
// refiner and the builders, and needs at least one of each; a fleet without
// one of them would boot, read the board, and wake nobody for the cards that
// role serves — a service smaller than the one the configuration commits to.
// The error names the profile the rule needs and the fleet lacks.
func servesRule(login map[string]string) error {
	carried := make(map[string]bool, len(login))
	for _, profile := range login {
		carried[profile] = true
	}
	if !carried[refinerProfile] {
		return fmt.Errorf("the fleet carries no refiner: the rule needs %s", refinerProfile)
	}
	for _, profile := range builderProfiles {
		if carried[profile] {
			return nil
		}
	}
	return fmt.Errorf("the fleet carries no builder: the rule needs one of %s", strings.Join(builderProfiles, ", "))
}

// attempts is how many times a look tries a failing action before it
// dead-letters it (SYSTEMS.md, Failure modes): one immediate retry for a
// transient answer, and the next look the patient one. The runner's error
// means nothing started, so a retried start cannot spawn twice; its own
// one-run-per-agent rule is the backstop.
const attempts = 2

// need is what one card asks for: the rule named an agent for it, and kind
// says where the agent's identity lives. The pass resolves kinds into
// wakes, and no kind sets a profile before the pass can start one.
type need struct {
	card  board.Card
	kind  string // "refiner", "builder", "assignee", "repair"
	login string // the assignee an assignee or a repair wake restarts
}

// wake is one agent's run the pass may start: the card the rule named for
// it, the login it must be claimed as, the fixed reason the run takes, and
// write — which says the pass claims the card before the run.
type wake struct {
	profile string
	login   string
	reason  string
	write   bool
	card    board.Card
}

// look is one state pass, whole: it reads the board, asks every card the
// rule, claims one wake per agent it has room for, and answers the trigger.
// A card whose claim the board refuses is walked past, not left holding the
// agent's one wake for the look: the pass claims again with that agent's
// slot released, so the next card naming it is reached in this same look.
// The delivery trigger carries an answer for its sender; the other triggers
// answer nobody and their lines leave the answer absent. The pass runs
// under the mutex: no two looks decide against the same records.
func (r *Router) look(ctx context.Context, trigger string, deliveries []string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	at := r.now().UTC()
	answer := http.StatusAccepted
	var woke []audit.Line
	var failed bool

	line := audit.Line{Trigger: trigger, Deliveries: deliveries}
	cards, err := tryRead(ctx, r.reader)
	if err != nil {
		err = fmt.Errorf("read the board: %w", err)
		line.Decision = "refused"
		if trigger == "delivery" {
			line.Answer = http.StatusBadGateway
		}
		// A refused look is still a look, and the line is the audit's
		// record of it; the same line, with the reason beside it, is the
		// dead-letter file's (SYSTEMS.md, step 9; ADR 006).
		r.log.Append(line)
		r.miss(err, audit.Miss{Reason: err.Error(), Line: line})
		return http.StatusBadGateway
	}

	held := make(map[string]bool)
	for _, card := range cards {
		// A hold is what PRODUCT fixes it as: one card per developer until
		// its pull request merges, and the merge is what moves it to
		// Finished. A Finished card's assignee is the record of work done,
		// not a claim on the developer it names.
		if card.Assignee != "" && (card.Column == "Refined" || card.Column == "Started") {
			held[card.Assignee] = true
		}
	}

	// The claims run in rounds: a round hands every agent it has room for
	// one card, and an agent whose write the board refused keeps its slot
	// for the next round, which claims the next card naming it. A card
	// claimed once is never claimed again in the same look, and a card is
	// its identity: two cards answering one number are two cards.
	asks := r.needs(cards, at)
	decided := make(map[string]bool)   // the profiles this look has served
	claimed := make(map[identity]bool) // the cards this look has claimed
	for {
		wakes := r.claim(asks, held, decided, claimed, at)
		if len(wakes) == 0 {
			break
		}
		for _, w := range wakes {
			call := w
			claimed[ident(call.card)] = true
			if call.write {
				if err := tryOnce(func() error { return r.writer.Assign(ctx, call.card.Repo, call.card.Number, call.login) }); err != nil {
					failed = true
					// The write is the claim, and it was refused: the card
					// stays unclaimed, so this agent's one wake for the look
					// is not spent on it — the next card naming the agent is
					// claimed in this same look, and this card's retry is the
					// next look.
					delete(decided, call.profile)
					r.fail(trigger, deliveries, fmt.Errorf("assign card %s: %w", ident(call.card), err), call.card.Number, call.login)
					continue
				}
			}
			run, session, err := tryBuild(func() (string, string, error) { return r.runner.Start(ctx, call.profile, call.reason) })
			if err != nil {
				failed = true
				// The claim was written and the run never went: the record says
				// so, and the next look restarts the run instead of deciding
				// the card again — the write is not repeated.
				r.attempted[ident(call.card)] = attempt{column: call.card.Column, assignee: call.login, ran: false}
				r.fail(trigger, deliveries, fmt.Errorf("start the run for %s on card %s: %w", call.profile, ident(call.card), err), call.card.Number, call.login)
				continue
			}
			// The claim is recorded on the state the pass leaves behind: the
			// column it read, the assignee it wrote, and that the run went.
			r.attempted[ident(call.card)] = attempt{column: call.card.Column, assignee: call.login, ran: true}
			woke = append(woke, audit.Line{
				Trigger:    trigger,
				Deliveries: deliveries,
				Decision:   fmt.Sprintf("woke %s for card %s", call.profile, ident(call.card)),
				Card:       call.card.Number,
				Assignee:   call.login,
				Run:        run,
				Session:    session,
			})
		}
	}

	if failed {
		answer = http.StatusBadGateway
	}
	for i := range woke {
		if trigger == "delivery" {
			woke[i].Answer = answer
		}
		if err := r.log.Append(woke[i]); err != nil {
			// The wakes are started; failing the look back cannot unstart
			// them, and the miss channel is the same disk that could not
			// take the line. The record is lost here, named by this branch.
			continue
		}
	}
	if len(woke) == 0 && !failed {
		line.Decision = "nothing to wake"
		if trigger == "delivery" {
			line.Answer = answer
		}
		r.log.Append(line)
	}
	return answer
}

// needs runs the rule over the pass's cards, in the order the read gave
// the board, and answers the cards that name an agent for their next step.
func (r *Router) needs(cards []board.Card, at time.Time) []need {
	var asks []need
	for _, card := range cards {
		if ask, ok := r.request(card, at); ok {
			asks = append(asks, ask)
		}
	}
	return asks
}

// request asks one card the rule: what moves it forward, who does it, and
// is it already happening. The answer is false when the card waits on
// nobody; the one question the memory answers comes first, because an
// action the pass took and left unfinished is attempted again before the
// rules decide.
func (r *Router) request(card board.Card, at time.Time) (need, bool) {
	if old, kept := r.attempted[ident(card)]; kept && old.column == card.Column && old.assignee == card.Assignee {
		if old.ran {
			// The wake ran and the card has not changed since: the action
			// has been already started, and a card its agent has been given
			// and has not moved is the agent's decision, not a wake.
			return need{}, false
		}
		// The wake never started its run, and a wake that started nothing
		// repairs nothing: the claim is already on the board, the run is
		// what restarts.
		return need{card: card, kind: "repair", login: card.Assignee}, true
	}
	switch card.Column {
	case "Backlog":
		if card.Assignee != "" {
			return need{}, false
		}
		return need{card: card, kind: "refiner"}, true
	case "Refined":
		if card.Assignee != "" {
			return need{}, false
		}
		return need{card: card, kind: "builder"}, true
	case "Started":
		if card.Assignee == "" {
			return need{}, false
		}
		return need{card: card, kind: "assignee", login: card.Assignee}, true
	}
	// Finished, and every column the rule does not answer: a card whose
	// next step needs the operator waits for the operator's ticket.
	return need{}, false
}

// claim turns the rule's names into wakes: at most one per agent per look,
// the lowest-numbered card that names it — the number first, the
// repository's name after it — and never for a profile with a run in flight.
// decided is the profiles this look has already served and claimed the
// identities it has already claimed; the look owns both. A profile whose
// claim the board refused is deleted from decided by the look that refused
// it, so the next card naming that agent is claimed in the same look. The
// pass writes no claim for an agent it cannot start — the cards it skips stay
// unclaimed, and the next look finds them.
func (r *Router) claim(asks []need, held map[string]bool, decided map[string]bool, claimed map[identity]bool, at time.Time) []wake {
	sort.SliceStable(asks, func(i, j int) bool {
		if asks[i].card.Number != asks[j].card.Number {
			return asks[i].card.Number < asks[j].card.Number
		}
		return asks[i].card.Repo < asks[j].card.Repo
	})
	var wakes []wake
	for _, ask := range asks {
		if claimed[ident(ask.card)] {
			continue
		}
		switch ask.kind {
		case "refiner":
			login, ok := r.loginOf(refinerProfile)
			if !ok || decided[refinerProfile] || r.runner.Running(refinerProfile) {
				continue
			}
			decided[refinerProfile] = true
			wakes = append(wakes, wake{
				profile: refinerProfile,
				login:   login,
				reason:  reasonBacklog(ident(ask.card), at),
				write:   true,
				card:    ask.card,
			})
		case "builder":
			profile, login, ok := r.freeBuilder(held, decided)
			if !ok {
				continue
			}
			decided[profile] = true
			wakes = append(wakes, wake{
				profile: profile,
				login:   login,
				reason:  reasonRefined(ident(ask.card), at),
				write:   true,
				card:    ask.card,
			})
		case "assignee":
			profile, ok := r.login[ask.login]
			if !ok || decided[profile] || r.runner.Running(profile) {
				// A login the fleet map does not carry is the operator's, or
				// a stranger's; the service starts neither.
				continue
			}
			decided[profile] = true
			wakes = append(wakes, wake{
				profile: profile,
				login:   ask.login,
				reason:  reasonStarted(ident(ask.card), at),
				write:   false,
				card:    ask.card,
			})
		case "repair":
			profile, ok := r.login[ask.login]
			if !ok || decided[profile] || r.runner.Running(profile) {
				continue
			}
			decided[profile] = true
			wakes = append(wakes, wake{
				profile: profile,
				login:   ask.login,
				reason:  reasonFor(ask.card.Column, ident(ask.card), at),
				write:   false,
				card:    ask.card,
			})
		}
	}
	return wakes
}

// freeBuilder is the free developer this look assigns: a builder in the
// fleet, holding no card in Refined or Started, without a run in flight,
// and not already decided by the look. The first by login wins, so a look
// is decided the same way every time.
func (r *Router) freeBuilder(held map[string]bool, decided map[string]bool) (string, string, bool) {
	var free []string
	for _, profile := range builderProfiles {
		if login, ok := r.loginOf(profile); ok && !held[login] && !decided[profile] && !r.runner.Running(profile) {
			free = append(free, login)
		}
	}
	sort.Strings(free)
	if len(free) == 0 {
		return "", "", false
	}
	return r.login[free[0]], free[0], true
}

// loginOf is the login the fleet map gives one profile. A profile with no
// login in the map stands for nobody: the pass wakes nobody for it.
func (r *Router) loginOf(profile string) (string, bool) {
	for login, name := range r.login {
		if name == profile {
			return login, true
		}
	}
	return "", false
}

// reasonFor is the fixed reason of the rule a column's wake answers; a
// repair reuses it, because the state it repairs is the same one.
func reasonFor(column string, card identity, at time.Time) string {
	switch column {
	case "Backlog":
		return reasonBacklog(card, at)
	case "Refined":
		return reasonRefined(card, at)
	case "Started":
		return reasonStarted(card, at)
	}
	return ""
}

// The wake reasons, one fixed shape per rule (ADR 009): identifiers the
// service owns and words it wrote, so the wording is reviewed here and
// every wake of a rule says the same thing. A payload field never reaches
// one: the audit line carries the delivery, the reason carries the card.

// reasonBacklog is the reason a Backlog card's refiner is woken with.
func reasonBacklog(card identity, at time.Time) string {
	return fmt.Sprintf("card %s sits in Backlog with nobody holding it; refine it; looked at %s", card, at.Format(time.RFC3339))
}

// reasonRefined is the reason a Refined card's developer is woken with.
func reasonRefined(card identity, at time.Time) string {
	return fmt.Sprintf("card %s sits in Refined with nobody holding it; build it; looked at %s", card, at.Format(time.RFC3339))
}

// reasonStarted is the reason a Started card's assignee is woken with.
func reasonStarted(card identity, at time.Time) string {
	return fmt.Sprintf("card %s sits in Started with no run in flight; finish or restart it; looked at %s", card, at.Format(time.RFC3339))
}

// tryRead reads the board, up to attempts times, and reports the last
// error, since a read both feeds and refuses the pass.
func tryRead(ctx context.Context, reader Reader) ([]board.Card, error) {
	var cards []board.Card
	var err error
	for range attempts {
		cards, err = reader.ReadAll(ctx)
		if err == nil {
			return cards, nil
		}
	}
	return nil, err
}

// tryOnce tries one failing action, up to attempts times, and reports its
// last error.
func tryOnce(op func() error) error {
	var err error
	for range attempts {
		err = op()
		if err == nil {
			return nil
		}
	}
	return err
}

// tryBuild tries starting the run, up to attempts times. The runner's
// error means nothing started, so a retried start cannot spawn twice.
func tryBuild(op func() (string, string, error)) (string, string, error) {
	var run, session string
	var err error
	for range attempts {
		run, session, err = op()
		if err == nil {
			return run, session, nil
		}
	}
	return "", "", err
}

// fail dead-letters one look's miss: the reason, in the service's own
// words, and the line the look would have appended. The pass never drops
// a wake it refused; the next look is the retry.
func (r *Router) fail(trigger string, deliveries []string, err error, card int, assignee string) {
	line := audit.Line{Trigger: trigger, Deliveries: deliveries, Decision: "refused", Card: card, Assignee: assignee}
	if trigger == "delivery" {
		line.Answer = http.StatusBadGateway
	}
	r.miss(err, audit.Miss{Reason: err.Error(), Line: line})
}

// miss writes one miss to the dead-letter file. A write that fails is
// dropped and named on stderr, not retried forever: the record cannot
// serve the look back to itself, and the next look re-attempts the action
// the miss was about.
func (r *Router) miss(err error, miss audit.Miss) {
	if writeErr := r.log.Dead(miss); writeErr != nil {
		fmt.Fprintln(os.Stderr, "dispatcher: dead-letter not written:", writeErr)
	}
}
