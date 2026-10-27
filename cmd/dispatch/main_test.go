package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aivara-se/dispatch/internal/audit"
	"github.com/aivara-se/dispatch/internal/receiver"
	"github.com/aivara-se/dispatch/internal/run"
)

// TestMain lets the test binary stand in for hermes: a child started with
// --profile writes the environment it inherited instead of running tests.
func TestMain(m *testing.M) {
	if out := os.Getenv("DISPATCH_TEST_ENVIRON"); out != "" && len(os.Args) > 1 && os.Args[1] == "--profile" {
		if err := os.WriteFile(out, []byte(strings.Join(os.Environ(), "\n")), 0o600); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// noRun starts nothing: the receiver is asked only whether it verifies.
type noRun struct{}

func (noRun) Start(string, string, string, func(int)) error { return nil }

// noRecord keeps the receiver's audit line out of the tree the test runs in.
type noRecord struct{}

func (noRecord) Append(audit.Record) error { return nil }

// TestARunCannotReadTheSecret pins #151: the service keeps the secret in memory,
// and the child it starts finds neither its name nor its value in the
// environment it inherits, so no value the child read signs a delivery.
func TestARunCannotReadTheSecret(t *testing.T) {
	const secret = "probe-webhook-secret-9f3c1d7a"
	dir := t.TempDir()
	dump := filepath.Join(dir, "environ")

	t.Setenv("DISPATCH_WEBHOOK_SECRET", secret)
	t.Setenv("DISPATCH_TEST_ENVIRON", dump)

	configPath := filepath.Join(dir, "dispatch.json")
	document := fmt.Sprintf(`{
  "listen": "127.0.0.1:8645",
  "path": "/github",
  "secret_name": "DISPATCH_WEBHOOK_SECRET",
  "hermes": %q,
  "agents": [{ "login": "thani-sh-meme", "profile": "meme" }]
}`, os.Args[0])
	if err := os.WriteFile(configPath, []byte(document), 0o600); err != nil {
		t.Fatalf("write configuration: %v", err)
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if string(cfg.Secret) != secret {
		t.Fatal("the service did not keep the secret in memory")
	}

	exits := make(chan int, 1)
	err = run.New(os.Args[0]).Start("meme", "Dispatch: a reason", "aivara-se/dispatch#151", func(exit int) { exits <- exit })
	if err != nil {
		t.Fatalf("start the child: %v", err)
	}
	select {
	case <-exits:
	case <-time.After(10 * time.Second):
		t.Fatal("the child never exited")
	}

	inherited, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("read the child's environment: %v", err)
	}
	child := string(inherited)
	if strings.Contains(child, "DISPATCH_WEBHOOK_SECRET") {
		t.Fatalf("the child's environment names the secret:\n%s", child)
	}
	if strings.Contains(child, secret) {
		t.Fatalf("the child's environment carries the secret's value:\n%s", child)
	}

	rc := receiver.New(cfg.Secret, cfg.AgentProfiles(), noRun{}, noRecord{})
	body := `{"action":"assigned","repository":{"full_name":"aivara-se/dispatch"},"issue":{"number":151},"assignee":{"login":"thani-sh-meme"},"sender":{"login":"thani-sh"}}`
	if got := signedPost(rc, "the-service", body, secret); got != http.StatusAccepted {
		t.Fatalf("a delivery signed with the service's own copy answered %d, want 202", got)
	}
	read := 0
	for i, line := range strings.Split(child, "\n") {
		_, value, ok := strings.Cut(line, "=")
		if !ok || value == "" {
			continue
		}
		read++
		if got := signedPost(rc, fmt.Sprintf("the-child-%d", i), body, value); got != http.StatusUnauthorized {
			t.Fatalf("a delivery signed with %q, a value the child read, answered %d, want 401", line, got)
		}
	}
	if read == 0 {
		t.Fatal("the child's environment was empty, so nothing was checked")
	}
}

// signedPost answers the status the receiver gives body signed with key.
func signedPost(rc *receiver.Receiver, delivery, body, key string) int {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(body))
	req := httptest.NewRequest(http.MethodPost, "/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", delivery)
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	w := httptest.NewRecorder()
	rc.ServeHTTP(w, req)
	return w.Code
}
