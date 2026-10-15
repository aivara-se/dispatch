// Command dispatcher boots the service: it loads the committed
// configuration, resolves the secrets the configuration names, opens the
// audit log, and serves the loopback port until it is stopped.
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
	"syscall"
	"time"

	"github.com/aivara-se/dispatcher/internal/audit"
	"github.com/aivara-se/dispatcher/internal/config"
)

// shutdownBound is how long the process waits for its listener at shutdown
// before it returns anyway.
const shutdownBound = 5 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "dispatcher:", err)
		os.Exit(1)
	}
}

// run boots the service from args and blocks until ctx is done. A
// configuration that cannot be decoded, a secret name that resolves to
// nothing, and log files that cannot be opened all stop the process here,
// before it serves anything.
func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("dispatcher", flag.ContinueOnError)
	configPath := flags.String("config", "", "path of the committed configuration file")
	auditPath := flags.String("audit", "dispatcher-audit.jsonl", "path of the audit file; the dead-letter file sits beside it")
	maxBytes := flags.Int64("max-bytes", 10<<20, "size each log file rotates at, in bytes")
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
	// The names are resolved at boot so that a reference which resolves to
	// nothing stops the process here; the values are consumed by the receiver
	// when it lands.
	if _, err := cfg.Secrets(lookupEnv); err != nil {
		return err
	}
	logFile, err := audit.Open(*auditPath, *maxBytes)
	if err != nil {
		return err
	}
	defer logFile.Close()
	return serve(ctx, cfg)
}

// lookupEnv resolves a secret's name to its value in the environment.
func lookupEnv(name string) ([]byte, bool) {
	value, ok := os.LookupEnv(name)
	return []byte(value), ok
}

// serve binds the configured address and serves it until ctx is done, then
// shuts down. Nothing is routed yet, so every path is served by nobody and
// answers 404, until the receiver lands and registers the configured path.
func serve(ctx context.Context, cfg config.Config) error {
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Addr, err)
	}
	log.Printf("dispatcher listening on %s", listener.Addr())
	mux := http.NewServeMux()
	server := &http.Server{Handler: mux}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()
	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownBound)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}
