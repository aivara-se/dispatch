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
	mux := http.NewServeMux()
	mux.Handle(cfg.Path, receiver.New(cfg.Secret, cfg.AgentProfiles(), run.New(cfg.Hermes, cfg.ChildEnvironment), record))

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	if err := record.Append(audit.BootRecord(cfg.Listen, len(cfg.Agents), time.Now())); err != nil {
		return err
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(listener) }()
	log.Printf("dispatch listening on %s%s", cfg.Listen, cfg.Path)

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}

	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}
