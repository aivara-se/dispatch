// Command dispatch runs the webhook receiver.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aivara-se/dispatch/internal/audit"
	"github.com/aivara-se/dispatch/internal/config"
	"github.com/aivara-se/dispatch/internal/metrics"
	"github.com/aivara-se/dispatch/internal/receiver"
	"github.com/aivara-se/dispatch/internal/run"
)

// auditPath is where a local run writes its record, beside the process.
const auditPath = "audit.jsonl"

func main() {
	configPath := flag.String("config", "dispatch.json", "path to the configuration file")
	flag.Parse()

	if err := serve(*configPath); err != nil {
		log.Fatal(err)
	}
}

func serve(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	record, err := audit.Open(auditPath)
	if err != nil {
		return err
	}
	defer record.Close()

	instruments := metrics.New()
	mux := http.NewServeMux()
	mux.Handle(cfg.Path, receiver.New(cfg.Secret, cfg.AgentProfiles(), run.New(cfg.Hermes), record, instruments))

	scrapeMux := http.NewServeMux()
	scrapeMux.Handle("/metrics", instruments)

	srv := newServer(mux)
	srv.Addr = cfg.Listen
	scrape := newServer(scrapeMux)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	// A port collision stops the process at boot rather than moving the route
	// ([`020`](docs/adrs/020-telemetry-leaves-through-a-collector.md)).
	scraped, err := net.Listen("tcp", metrics.Addr)
	if err != nil {
		listener.Close()
		return err
	}
	if err := record.Append(audit.BootRecord(cfg.Listen, len(cfg.Agents), time.Now())); err != nil {
		return err
	}

	errc := make(chan error, 2)
	go func() { errc <- srv.Serve(listener) }()
	go func() { errc <- scrape.Serve(scraped) }()
	log.Printf("dispatch listening on %s%s", cfg.Listen, cfg.Path)
	log.Printf("dispatch serving metrics on %s/metrics", metrics.Addr)

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}

	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	scrape.Shutdown(shutdown)
	return srv.Shutdown(shutdown)
}

// newServer returns a server with the timeouts every listener here keeps.
func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
