package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// good is a complete, valid configuration; every refusal case below mutates
// one part of it.
const good = `{
	"addr": "127.0.0.1:8645",
	"path": "/github",
	"repos": ["aivara-se/dispatcher"],
	"events": ["issues", "pull_request"],
	"owner": "aivara-se",
	"project": 2,
	"board": "DISPATCHER_BOARD_TOKEN",
	"agents": [{"profile": "meme", "login": "thani-sh-meme"}],
	"routes": [{"name": "org", "secretEnv": "DISPATCHER_WEBHOOK_SECRET"}]
}`

// write puts content in a configuration file and returns its path.
func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dispatcher.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadReadsEveryField(t *testing.T) {
	want := Config{
		Addr:    "127.0.0.1:8645",
		Path:    "/github",
		Repos:   []string{"aivara-se/dispatcher"},
		Events:  []string{"issues", "pull_request"},
		Owner:   "aivara-se",
		Project: 2,
		Board:   "DISPATCHER_BOARD_TOKEN",
		Agents:  []Agent{{Profile: "meme", Login: "thani-sh-meme"}},
		Routes:  []Route{{Name: "org", SecretEnv: "DISPATCHER_WEBHOOK_SECRET"}},
	}
	got, err := Load(write(t, good))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("load gave %+v, want %+v", got, want)
	}
}

func TestLoadRefuses(t *testing.T) {
	cases := []struct {
		name, old, new, want string
	}{
		{"an unknown field", `"owner": "aivara-se"`, `"owner": "aivara-se", "address": "127.0.0.1:8645"`, "unknown field"},
		{"an empty addr", `"addr": "127.0.0.1:8645"`, `"addr": ""`, "addr is empty"},
		{"an empty path", `"path": "/github"`, `"path": ""`, "path is empty"},
		{"a path with no leading slash", `"path": "/github"`, `"path": "github"`, "no leading slash"},
		{"an empty owner", `"owner": "aivara-se"`, `"owner": ""`, "owner is empty"},
		{"a project that is no number", `"project": 2`, `"project": 0`, "not a project number"},
		{"a board with no secret name", `"board": "DISPATCHER_BOARD_TOKEN"`, `"board": ""`, "no secret name"},
		{"a route with an empty name", `"name": "org"`, `"name": ""`, "empty name"},
		{"a route with no secret name", `"secretEnv": "DISPATCHER_WEBHOOK_SECRET"`, `"secretEnv": ""`, "no secret name"},
		{"a route name twice",
			`"routes": [{"name": "org", "secretEnv": "DISPATCHER_WEBHOOK_SECRET"}]`,
			`"routes": [{"name": "org", "secretEnv": "A"}, {"name": "org", "secretEnv": "B"}]`,
			"appears twice"},
		{"an agent with an empty profile", `"profile": "meme"`, `"profile": ""`, "empty profile"},
		{"an agent with no login", `"login": "thani-sh-meme"`, `"login": ""`, "no login"},
		{"a profile twice",
			`"agents": [{"profile": "meme", "login": "thani-sh-meme"}]`,
			`"agents": [{"profile": "meme", "login": "a"}, {"profile": "meme", "login": "b"}]`,
			"appears twice"},
		{"a login twice",
			`"agents": [{"profile": "meme", "login": "thani-sh-meme"}]`,
			`"agents": [{"profile": "meme", "login": "thani-sh-meme"}, {"profile": "mimi", "login": "thani-sh-meme"}]`,
			"appears twice"},
		{"a file that is not JSON", `"owner": "aivara-se"`, `"owner": `, "decode config"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(write(t, strings.Replace(good, c.old, c.new, 1)))
			if err == nil {
				t.Fatalf("load accepted the configuration")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not say %q", err, c.want)
			}
		})
	}
}

func TestLoadRefusesAMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err == nil || !strings.Contains(err.Error(), "read config") {
		t.Errorf("error %v does not name the read", err)
	}
}

func TestLoadRefusesAnEmptyFile(t *testing.T) {
	_, err := Load(write(t, ""))
	if err == nil || !strings.Contains(err.Error(), "no JSON object") {
		t.Errorf("error %v does not name the empty file", err)
	}
}

func TestLoadRefusesDataAfterTheObject(t *testing.T) {
	_, err := Load(write(t, good+" {}"))
	if err == nil || !strings.Contains(err.Error(), "unexpected data") {
		t.Errorf("error %v does not name the trailing data", err)
	}
}

func TestSecretsResolvesEveryRoute(t *testing.T) {
	cfg := Config{Routes: []Route{
		{Name: "org", SecretEnv: "ORG_SECRET"},
		{Name: "repo", SecretEnv: "REPO_SECRET"},
	}}
	values := map[string]string{"ORG_SECRET": "org-value", "REPO_SECRET": "repo-value"}
	got, err := cfg.Secrets(func(name string) ([]byte, bool) {
		value, ok := values[name]
		return []byte(value), ok
	})
	if err != nil {
		t.Fatalf("secrets: %v", err)
	}
	want := map[string][]byte{"org": []byte("org-value"), "repo": []byte("repo-value")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("secrets gave %#v, want %#v", got, want)
	}
}

func TestSecretsRefusesANameThatResolvesToNothing(t *testing.T) {
	cfg := Config{Routes: []Route{{Name: "org", SecretEnv: "ORG_SECRET"}}}
	_, err := cfg.Secrets(func(string) ([]byte, bool) { return nil, false })
	if err == nil {
		t.Fatal("secrets accepted a name that resolves to nothing")
	}
	if !strings.Contains(err.Error(), "org") || !strings.Contains(err.Error(), "ORG_SECRET") {
		t.Errorf("error %q names neither the route nor the secret", err)
	}
}

func TestSecretsRefusesANameThatResolvesToAnEmptyValue(t *testing.T) {
	cfg := Config{Routes: []Route{{Name: "org", SecretEnv: "ORG_SECRET"}}}
	_, err := cfg.Secrets(func(string) ([]byte, bool) { return []byte{}, true })
	if err == nil {
		t.Fatal("secrets accepted a name that resolves to an empty value")
	}
}
