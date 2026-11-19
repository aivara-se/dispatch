// Command dispatch runs the engine's webhook server and its record API.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/aivara-se/dispatch/internal/api"
	"github.com/aivara-se/dispatch/internal/condition"
	"github.com/aivara-se/dispatch/internal/config"
	"github.com/aivara-se/dispatch/internal/engine"
	"github.com/aivara-se/dispatch/internal/github"
	"github.com/aivara-se/dispatch/internal/hermes"
	"github.com/aivara-se/dispatch/internal/record"
	"github.com/aivara-se/dispatch/internal/runner"
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
	socketPath := flags.String("socket", "", "path to the API socket (default ~/.dispatch/dispatch.sock)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if *dbPath == "" {
		*dbPath = filepath.Join(home, ".dispatch", "dispatch.db")
	}
	if *socketPath == "" {
		*socketPath = filepath.Join(home, ".dispatch", "dispatch.sock")
	}

	cfg, flows, err := config.Load(*configDir)
	if err != nil {
		return err
	}
	secret, ok := os.LookupEnv(cfg.SecretName)
	if !ok || secret == "" {
		return fmt.Errorf("%s: not set", cfg.SecretName)
	}
	keyPEM, err := os.ReadFile(cfg.KeyPath)
	if err != nil {
		return fmt.Errorf("key_path: %w", err)
	}
	app, err := github.New(cfg.AppID, keyPEM, "", nil)
	if err != nil {
		return err
	}

	store, err := record.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	names := make([]string, 0, len(flows))
	for _, flow := range flows {
		names = append(names, flow.Name)
	}
	hub := api.NewHub()
	profiles := make(map[string]string, len(cfg.Agents))
	for name, agent := range cfg.Agents {
		profiles[name] = agent.Profile
	}
	jobRunner := runner.New(store, flows, *configDir, profiles,
		hermes.New(cfg.Hermes), condition.New(app), app, hub.Publish)
	handler := webhook.NewHandler([]byte(secret), engine.New(store, cfg.Repos, flows, app, hub.Publish, jobRunner).Deliver)

	webhookServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           pathHandler(cfg.Path, handler),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := os.MkdirAll(filepath.Dir(*socketPath), 0o700); err != nil {
		return err
	}
	if _, err := net.DialTimeout("unix", *socketPath, 200*time.Millisecond); err == nil {
		return fmt.Errorf("socket %s is in use", *socketPath)
	}
	if err := os.Remove(*socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.Listen("unix", *socketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(*socketPath, 0o600); err != nil {
		listener.Close()
		return err
	}
	apiServer := &http.Server{Handler: api.New(store, names, hub).Handler()}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := jobRunner.Resume(ctx); err != nil {
		return err
	}

	served := make(chan error, 2)
	go func() {
		log.Printf("dispatch: listening on %s%s", cfg.Listen, cfg.Path)
		served <- webhookServer.ListenAndServe()
	}()
	go func() {
		log.Printf("dispatch: serving %s", *socketPath)
		served <- apiServer.Serve(listener)
	}()

	select {
	case err := <-served:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}

	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := apiServer.Shutdown(shutdown); err != nil {
		return err
	}
	if err := webhookServer.Shutdown(shutdown); err != nil {
		return err
	}
	// Answering 202 queues the work, so a stop waits for the queue to apply
	// before the record closes under it.
	if err := handler.Wait(shutdown); err != nil {
		return fmt.Errorf("deliveries at shutdown: %w", err)
	}
	jobRunner.Close()
	return nil
}

func pathHandler(path string, handler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	return mux
}
