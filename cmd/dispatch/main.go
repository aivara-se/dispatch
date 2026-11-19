// Command dispatch runs the engine's webhook server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/aivara-se/dispatch/internal/config"
	"github.com/aivara-se/dispatch/internal/engine"
	"github.com/aivara-se/dispatch/internal/record"
	"github.com/aivara-se/dispatch/internal/webhook"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("dispatch", flag.ContinueOnError)
	configDir := flags.String("config", "config", "directory holding dispatch.json and flows/")
	dbPath := flags.String("db", "", "path to the SQLite record (default ~/.dispatch/dispatch.db)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		*dbPath = filepath.Join(home, ".dispatch", "dispatch.db")
	}

	cfg, flows, err := config.Load(*configDir)
	if err != nil {
		return err
	}
	secret, ok := os.LookupEnv(cfg.SecretName)
	if !ok || secret == "" {
		return fmt.Errorf("%s: not set", cfg.SecretName)
	}

	store, err := record.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	handler := webhook.NewHandler([]byte(secret), engine.New(store, cfg.Repos, flows).Deliver)
	mux := http.NewServeMux()
	mux.Handle(cfg.Path, handler)
	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	served := make(chan error, 1)
	go func() {
		log.Printf("dispatch: listening on %s%s", cfg.Listen, cfg.Path)
		served <- server.ListenAndServe()
	}()

	select {
	case err := <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}
