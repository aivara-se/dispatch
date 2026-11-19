package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aivara-se/dispatch/internal/config"
)

const goodDispatch = `{
  "listen": "127.0.0.1:8645",
  "path": "/github",
  "secret_name": "DISPATCH_WEBHOOK_SECRET",
  "repos": ["aivara-se/*"],
  "hermes": "/usr/local/bin/hermes",
  "agents": { "memo": { "profile": "memo" } }
}`

const goodFlow = `{
  "key": "{n}",
  "tasks": {
    "_start_": { "next": { "pr.created": "review" } },
    "review": {
      "actions": [
        { "type": "prompt", "agent": "memo", "text": "Review {pr.url}." }
      ],
      "limit": { "turn": 1, "time": 60 },
      "next": { "pr.approved": "follow", "pr.rejected": "_done_" }
    },
    "follow": {
      "actions": [],
      "limit": { "turn": 1, "time": 60 },
      "next": { "pr.merged": "_done_" }
    }
  }
}`

func TestLoadAcceptsTheExample(t *testing.T) {
	cfg, flows, err := config.Load("../../config")
	if err != nil {
		t.Fatalf("load example: %v", err)
	}
	if cfg.Listen != "127.0.0.1:8645" || cfg.Path != "/github" || cfg.SecretName != "DISPATCH_WEBHOOK_SECRET" {
		t.Fatalf("server fields = %q %q %q", cfg.Listen, cfg.Path, cfg.SecretName)
	}
	if len(flows) != 1 || flows[0].Name != "review" {
		t.Fatalf("flows = %+v", flows)
	}
	if flows[0].Key != "{n}" {
		t.Fatalf("key = %q", flows[0].Key)
	}
	if len(flows[0].Start) != 1 || flows[0].Start[0] != (config.Condition{Key: "pr.created", Target: "review"}) {
		t.Fatalf("start = %+v", flows[0].Start)
	}
	if got := flows[0].Tasks["review"].Limit; got.Turn != 10 || got.Time != 1800 {
		t.Fatalf("limit = %+v", got)
	}
}

func TestLoadKeepsNextOrder(t *testing.T) {
	_, flows, err := load(t, goodDispatch, map[string]string{"review": goodFlow})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	next := flows[0].Tasks["review"].Next
	if len(next) != 2 || next[0].Key != "pr.approved" || next[1].Key != "pr.rejected" {
		t.Fatalf("next = %+v", next)
	}
}

func TestLoadRefusesInvalidConfiguration(t *testing.T) {
	cases := []struct {
		name     string
		dispatch string
		flow     string
		file     string
		key      string
	}{
		{
			name: "unknown field in dispatch",
			dispatch: strings.Replace(goodDispatch, `"repos"`, `"extra": true,
  "repos"`, 1),
			flow: goodFlow,
			file: "dispatch.json",
			key:  "extra",
		},
		{
			name:     "unknown field in a task",
			dispatch: goodDispatch,
			flow:     strings.Replace(goodFlow, `"limit": { "turn": 1, "time": 60 },`, `"steps": [], "limit": { "turn": 1, "time": 60 },`, 1),
			file:     "review.json",
			key:      "tasks.review.steps",
		},
		{
			name:     "unknown condition",
			dispatch: goodDispatch,
			flow:     strings.Replace(goodFlow, `"pr.created": "review"`, `"pr.opened": "review"`, 1),
			file:     "review.json",
			key:      "tasks._start_.next.pr.opened",
		},
		{
			name:     "start on merged",
			dispatch: goodDispatch,
			flow:     strings.Replace(goodFlow, `"pr.created": "review"`, `"pr.merged": "review"`, 1),
			file:     "review.json",
			key:      "tasks._start_.next.pr.merged",
		},
		{
			name:     "start on updated",
			dispatch: goodDispatch,
			flow:     strings.Replace(goodFlow, `"pr.created": "review"`, `"pr.updated": "review"`, 1),
			file:     "review.json",
			key:      "tasks._start_.next.pr.updated",
		},
		{
			name:     "unknown target",
			dispatch: goodDispatch,
			flow:     strings.Replace(goodFlow, `"pr.created": "review"`, `"pr.created": "missing"`, 1),
			file:     "review.json",
			key:      "tasks._start_.next.pr.created",
		},
		{
			name:     "missing limit",
			dispatch: goodDispatch,
			flow:     strings.Replace(goodFlow, `"limit": { "turn": 1, "time": 60 },`, ``, 1),
			file:     "review.json",
			key:      "tasks.review.limit",
		},
		{
			name:     "unknown agent",
			dispatch: goodDispatch,
			flow:     strings.Replace(goodFlow, `"agent": "memo"`, `"agent": "ghost"`, 1),
			file:     "review.json",
			key:      "tasks.review.actions[0].agent",
		},
		{
			name:     "actions on start",
			dispatch: goodDispatch,
			flow:     strings.Replace(goodFlow, `"_start_": { "next"`, `"_start_": { "actions": [], "next"`, 1),
			file:     "review.json",
			key:      "tasks._start_.actions",
		},
		{
			name:     "key is two segments",
			dispatch: goodDispatch,
			flow:     strings.Replace(goodFlow, `"key": "{n}"`, `"key": "a/{n}"`, 1),
			file:     "review.json",
			key:      "key",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := load(t, tc.dispatch, map[string]string{"review": tc.flow})
			if err == nil {
				t.Fatal("load succeeded")
			}
			var configErr *config.Error
			if !errors.As(err, &configErr) {
				t.Fatalf("error is not a config error: %v", err)
			}
			if filepath.Base(configErr.File) != tc.file {
				t.Fatalf("file = %q, want %q (%v)", configErr.File, tc.file, err)
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("error %q does not name %q", err, tc.key)
			}
		})
	}
}

func TestLoadRefusesNonAbsoluteHermes(t *testing.T) {
	dispatch := strings.Replace(goodDispatch, `"/usr/local/bin/hermes"`, `"hermes"`, 1)
	_, _, err := load(t, dispatch, map[string]string{"review": goodFlow})
	if err == nil || !strings.Contains(err.Error(), "hermes") {
		t.Fatalf("error = %v", err)
	}
}

func load(t *testing.T, dispatch string, flows map[string]string) (*config.Config, []config.Flow, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "flows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dispatch.json"), []byte(dispatch), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, body := range flows {
		if err := os.WriteFile(filepath.Join(dir, "flows", name+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return config.Load(dir)
}
