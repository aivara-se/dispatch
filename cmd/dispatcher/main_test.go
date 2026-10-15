package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aivara-se/dispatcher/internal/config"
)

// auditPath puts the audit file where the test's own scratch directory holds
// it, never in the working tree.
func auditPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "audit.jsonl")
}

func TestRunBootsWithTheFixtureConfig(t *testing.T) {
	t.Setenv("DISPATCHER_WEBHOOK_SECRET", "fixture-value")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	args := []string{"--config", "testdata/dispatcher.json", "--audit", auditPath(t)}
	go func() {
		done <- run(ctx, args)
	}()
	select {
	case err := <-done:
		t.Fatalf("run returned before it was stopped: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunStopsAtBootWhenASecretNameResolvesToNothing(t *testing.T) {
	os.Unsetenv("DISPATCHER_WEBHOOK_SECRET")
	err := run(context.Background(), []string{"--config", "testdata/dispatcher.json", "--audit", auditPath(t)})
	if err == nil {
		t.Fatal("run booted with a secret name that resolves to nothing")
	}
	if !strings.Contains(err.Error(), "DISPATCHER_WEBHOOK_SECRET") {
		t.Errorf("error %q does not name the secret", err)
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
	t.Setenv("DISPATCHER_WEBHOOK_SECRET", "fixture-value")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	cfg := config.Config{
		Addr:    listener.Addr().String(),
		Path:    "/github",
		Owner:   "aivara-se",
		Project: 2,
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
