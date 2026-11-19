package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const configFile = `{
  "listen": "127.0.0.1:0",
  "path": "/github",
  "secret_name": "DISPATCH_WEBHOOK_SECRET",
  "app_id": 123456,
  "key_path": "/tmp/app.pem",
  "repos": ["aivara-se/*"],
  "hermes": "/usr/local/bin/hermes",
  "agents": {}
}`

func TestRunRefusesMissingSecret(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dispatch.json"), []byte(configFile), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISPATCH_WEBHOOK_SECRET", "")

	dbPath := filepath.Join(dir, "dispatch.db")
	err := run([]string{"--config", dir, "--db", dbPath})
	if err == nil || !strings.Contains(err.Error(), "DISPATCH_WEBHOOK_SECRET") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(dbPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("record was opened without the secret: %v", err)
	}
}
