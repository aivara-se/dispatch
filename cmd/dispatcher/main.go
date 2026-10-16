// Command dispatcher boots the service: it loads the committed
// configuration, resolves the secrets the configuration names, opens the
// audit log, wires the five tracks the service is made of — the board, the
// receiver, the router, the runner and the audit — and serves the loopback
// port until it is stopped.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aivara-se/dispatcher/internal/audit"
	"github.com/aivara-se/dispatcher/internal/board"
	"github.com/aivara-se/dispatcher/internal/config"
	"github.com/aivara-se/dispatcher/internal/receiver"
	"github.com/aivara-se/dispatcher/internal/router"
	"github.com/aivara-se/dispatcher/internal/runner"
)

const (
	// shutdownBound is how long the process waits for its listener at
	// shutdown before it returns anyway.
	shutdownBound = 5 * time.Second
	// runBound is how long one run may live before the runner kills it.
	// Provisional: the first live run freezes the number (ADR 004).
	runBound = 30 * time.Minute
	// bodyLimit is the largest delivery read before refusal, the limit
	// ADR 002 fixes to match the gateway's own webhook adapter.
	bodyLimit = 1 << 20
	// perMinute is the inbound limit ADR 006 fixes. Provisional: the
	// first live week freezes the number.
	perMinute = 60
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "dispatcher:", err)
		os.Exit(1)
	}
}

// run boots the service from args and blocks until ctx is done. A
// configuration that cannot be decoded, a fleet that cannot serve the rule,
// a secret name that resolves to nothing, and log files that cannot be
// opened all stop the process here, before it serves anything.
func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("dispatcher", flag.ContinueOnError)
	configPath := flags.String("config", "", "path of the committed configuration file")
	auditPath := flags.String("audit", "dispatcher-audit.jsonl", "path of the audit file; the dead-letter file sits beside it")
	maxBytes := flags.Int64("max-bytes", 10<<20, "size each log file rotates at, in bytes")
	hermes := flags.String("hermes", "hermes", "path of the Hermes binary a run launches")
	github := flags.String("github", "https://api.github.com", "base of GitHub's API; the integration points a boot's board at a stub server")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return fmt.Errorf("--config is required")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	// The secrets are resolved at boot so that a reference which resolves
	// to nothing stops the process here; the route's secret is consumed
	// by the receiver when a delivery lands.
	secrets, err := cfg.Secrets(lookupEnv)
	if err != nil {
		return err
	}
	// One route is what this boot wires: the receiver verifies one path
	// with one secret, and a second source needs the surface that tells
	// its deliveries apart before anything more is promised (ADR 002
	// names the sources a route can be).
	if len(cfg.Routes) != 1 {
		return fmt.Errorf("config: %d routes are named, and this boot verifies exactly one", len(cfg.Routes))
	}
	// The board's token is the service's own credential, not a route's:
	// it does not vary with the sources, and the configuration names
	// where it lives the way a route's secret is named. A name that
	// resolves to nothing stops the boot.
	token, ok := lookupEnv(cfg.Board)
	if !ok || len(token) == 0 {
		return fmt.Errorf("board: secret %s resolves to nothing", cfg.Board)
	}
	logFile, err := audit.Open(*auditPath, *maxBytes)
	if err != nil {
		return err
	}
	// The log is not closed here: a look may still be writing when the
	// process is on the way out, and a line that is in the file when the
	// process exits is the record — every write reaches the file as it
	// happens, and the exit releases the file handles the Log holds.

	theBoard := &bridge{
		client:  board.NewAt(string(token), *github+"/graphql", *github),
		owner:   cfg.Owner,
		project: cfg.Project,
	}

	// The runner's end reaches the router's look: a look on a run's end is
	// one of the three moments ADR 008 fixes, and the pass serves the
	// boot's lifetime — a run outlives the HTTP request that carried its
	// wake, and nothing the request holds binds it.
	var pass *router.Router
	runs := runner.New(*hermes, runBound, func(profile string) {
		pass.OnRunEnd(ctx, profile)
	})
	pass, err = router.New(theBoard, theBoard, runs, logFile, fleet(cfg.Agents))
	if err != nil {
		return err
	}

	handler := receiver.New(
		[]byte(secrets[cfg.Routes[0].Name]),
		receiver.Allow{Repos: cfg.Repos, Events: cfg.Events},
		perMinute, bodyLimit,
		func(_ context.Context, delivery receiver.Delivery) int {
			return pass.OnDelivery(ctx, delivery)
		},
	)
	return serve(ctx, cfg, handler, func() { pass.OnBoot(ctx) })
}

// bridge is how the board client meets the router: the reader and writer one
// look decides from, bound to the configuration's owner and project. The
// router names the card the rule decided on by its repository and its number,
// and the repository is the one the board named for that card in the read
// before the write. The set one read fills is keyed by that identity, so two
// cards of one number are two entries and the write reaches the one the
// decision named.
type bridge struct {
	client  *board.Client
	owner   string
	project int

	placed map[card]bool
}

// card is one card's identity as the bridge holds it: the repository the board
// named and the issue's number, the pair that tells two cards of one project
// apart.
type card struct {
	repo   string
	number int
}

// ReadAll reads the whole board — the surface the router gives as Reader —
// and keeps the identity of every card it placed, so a write can name one.
func (b *bridge) ReadAll(ctx context.Context) ([]board.Card, error) {
	cards, err := b.client.ReadAll(ctx, b.owner, b.project)
	if err != nil {
		return nil, err
	}
	placed := make(map[card]bool, len(cards))
	for _, c := range cards {
		placed[card{repo: c.Repo, number: c.Number}] = true
	}
	b.placed = placed
	return cards, nil
}

// Assign writes one card's assignee — the surface the router gives as Writer
// — at the issue the card's identity names, the repository the read before it
// placed the card in. A card no read placed has no repository the board named
// for it, and the write is refused rather than pointed at a guess. The
// router's own miss names the card; these words carry the reason alone.
func (b *bridge) Assign(ctx context.Context, repo string, number int, login string) error {
	if !b.placed[card{repo: repo, number: number}] {
		return fmt.Errorf("no read placed card %d of %q, so the repository to write is unknown", number, repo)
	}
	owner, name, named := strings.Cut(repo, "/")
	if !named || owner == "" || name == "" {
		return fmt.Errorf("the board named %q for card %d, which is not a repository's owner and name", repo, number)
	}
	return b.client.Assign(ctx, owner, name, number, login)
}

// fleet maps each agent's GitHub login to the Hermes profile their run uses,
// the way the configuration names them.
func fleet(agents []config.Agent) map[string]string {
	login := make(map[string]string, len(agents))
	for _, agent := range agents {
		login[agent.Login] = agent.Profile
	}
	return login
}

// lookupEnv resolves a secret's name to its value in the environment.
func lookupEnv(name string) ([]byte, bool) {
	value, ok := os.LookupEnv(name)
	return []byte(value), ok
}

// serve binds the configured address, registers the configured path to the
// receiver — every other path answers 404 — and serves until ctx is done,
// then shuts down. The boot look fires once the listener is live, so a card
// no delivery names is asked the rule at boot the way ADR 008 fixes it.
func serve(ctx context.Context, cfg config.Config, handler http.Handler, onBoot func()) error {
	mux := http.NewServeMux()
	mux.Handle(cfg.Path, handler)
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Addr, err)
	}
	log.Printf("dispatcher listening on %s", listener.Addr())
	server := &http.Server{Handler: mux}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()
	onBoot()
	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownBound)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err == nil {
			return nil
		}
		// The bound passed with a connection still held — a keep-alive
		// client between requests is one — and it leaves with the
		// process rather than being served to the end. A delivery cut
		// here is a delivery GitHub answers with a retry, and the
		// restart's boot look is what repairs a wake that dropped.
		server.Close()
		return nil
	}
}
