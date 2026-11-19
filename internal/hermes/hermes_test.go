package hermes_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aivara-se/dispatch/internal/hermes"
	_ "modernc.org/sqlite"
)

func TestRunRecordsTheOneShot(t *testing.T) {
	binary := fakeBinary(t)
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("ARGS_FILE", argsFile)
	t.Setenv("FAKE_OUTPUT", "Reviewed the diff.")
	t.Setenv("FAKE_SESSION", "s1")
	t.Setenv("HERMES_HOME", stateHome(t, "s1"))

	runner := hermes.New(binary)
	result, err := runner.Run(context.Background(), "momo", "Review pull request.", "212")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Output != "Reviewed the diff." || result.Exit != 0 || result.Session != "s1" {
		t.Fatalf("result = %+v", result)
	}
	if result.Tools != `[{"name":"read"},{"name":"comment"}]` {
		t.Fatalf("tools = %s", result.Tools)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "--profile|momo|chat|-q|Review pull request.|-Q|--source|tool|-c|212|--create-if-missing|"
	if string(args) != want+"\n" {
		t.Fatalf("args = %q", args)
	}
}

func TestRunReportsANonZeroExit(t *testing.T) {
	binary := fakeBinary(t)
	t.Setenv("ARGS_FILE", filepath.Join(t.TempDir(), "args"))
	t.Setenv("FAKE_EXIT", "2")
	t.Setenv("HERMES_HOME", stateHome(t, "s1"))

	result, err := hermes.New(binary).Run(context.Background(), "memo", "Fix it.", "212")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Exit != 2 {
		t.Fatalf("exit = %d", result.Exit)
	}
}

func TestRunKillsTheChildAtTheDeadline(t *testing.T) {
	binary := fakeBinary(t)
	t.Setenv("ARGS_FILE", filepath.Join(t.TempDir(), "args"))
	t.Setenv("FAKE_SLEEP", "5")
	t.Setenv("HERMES_HOME", stateHome(t, "s1"))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := hermes.New(binary).Run(ctx, "memo", "Fix it.", "212")
	if err == nil {
		t.Fatal("the deadline was not reported")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("the child outlived the deadline: %s", time.Since(start))
	}
}

func TestMissingSessionLeavesToolsEmpty(t *testing.T) {
	binary := fakeBinary(t)
	t.Setenv("ARGS_FILE", filepath.Join(t.TempDir(), "args"))
	t.Setenv("FAKE_SESSION", "")
	t.Setenv("HERMES_HOME", t.TempDir())

	result, err := hermes.New(binary).Run(context.Background(), "memo", "Fix it.", "212")
	if err != nil {
		t.Fatal(err)
	}
	if result.Tools != "" {
		t.Fatalf("tools = %q", result.Tools)
	}
}

func TestRunReportsABrokenStore(t *testing.T) {
	binary := fakeBinary(t)
	t.Setenv("ARGS_FILE", filepath.Join(t.TempDir(), "args"))
	t.Setenv("FAKE_SESSION", "s1")
	home := t.TempDir()
	dir := filepath.Join(home, "profiles", "memo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.db"), []byte("not a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERMES_HOME", home)

	if _, err := hermes.New(binary).Run(context.Background(), "memo", "Fix it.", "212"); err == nil {
		t.Fatal("a store it cannot read passed as no tool calls")
	}
}

func fakeBinary(t *testing.T) string {
	t.Helper()
	source, err := os.ReadFile("testdata/hermes.sh")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "hermes")
	if err := os.WriteFile(path, source, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func stateHome(t *testing.T, session string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "profiles", "momo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE messages (id INTEGER PRIMARY KEY, session_id TEXT, tool_calls TEXT)`); err != nil {
		t.Fatal(err)
	}
	if session == "" {
		return home
	}
	_, err = db.Exec(`INSERT INTO messages (session_id, tool_calls) VALUES (?, ?), (?, ?)`,
		session, `[{"name":"read"}]`, session, `[{"name":"comment"}]`)
	if err != nil {
		t.Fatal(err)
	}
	return home
}
