package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const valid = `{
	"listen": "127.0.0.1:8645",
	"path": "/github",
	"secret_name": "DISPATCH_TEST_SECRET",
	"hermes": "/usr/local/bin/hermes",
	"child_env": ["HOME", "PATH"],
	"agents": [
		{"login": "thani-sh-meme", "profile": "meme"},
		{"login": "thani-sh-mama", "profile": "mama"}
	]
}`

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dispatch.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	t.Setenv("DISPATCH_TEST_SECRET", "fixture-secret-not-a-credential")

	c, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Listen != "127.0.0.1:8645" || c.Path != "/github" {
		t.Fatalf("listen/path = %q %q", c.Listen, c.Path)
	}
	if c.Hermes != "/usr/local/bin/hermes" {
		t.Fatalf("hermes = %q", c.Hermes)
	}
	if string(c.Secret) != "fixture-secret-not-a-credential" {
		t.Fatalf("secret = %q", c.Secret)
	}
	if got := c.ChildEnvironment; len(got) != 2 || got[0] != "HOME="+os.Getenv("HOME") || got[1] != "PATH="+os.Getenv("PATH") {
		t.Fatalf("child environment = %q", got)
	}
	if got := c.AgentProfiles()["thani-sh-meme"]; got != "meme" {
		t.Fatalf("profile for thani-sh-meme = %q", got)
	}
}

func TestLoadRefusals(t *testing.T) {
	cases := []struct {
		name    string
		content string
		env     bool
		want    string
	}{
		{
			name:    "unknown field",
			content: strings.Replace(valid, `"path"`, `"unknown": true, "path"`, 1),
			env:     true,
			want:    "unknown field",
		},
		{
			name:    "unresolvable secret",
			content: valid,
			env:     false,
			want:    "is not set in the environment",
		},
		{
			name:    "relative hermes",
			content: strings.Replace(valid, "/usr/local/bin/hermes", "./hermes", 1),
			env:     true,
			want:    "absolute path",
		},
		{
			name:    "duplicate login",
			content: strings.Replace(valid, `"thani-sh-mama", "profile": "mama"`, `"thani-sh-meme", "profile": "mama"`, 1),
			env:     true,
			want:    "listed twice",
		},
		{
			name:    "empty agents",
			content: `{"listen":"127.0.0.1:8645","path":"/github","secret_name":"DISPATCH_TEST_SECRET","hermes":"/usr/local/bin/hermes","child_env":["HOME"],"agents":[]}`,
			env:     true,
			want:    "agents is empty",
		},
		{
			name:    "trailing data",
			content: valid + `{}`,
			env:     true,
			want:    "trailing data",
		},
		{
			name:    "missing child_env",
			content: strings.Replace(valid, "\"child_env\": [\"HOME\", \"PATH\"],\n", "", 1),
			env:     true,
			want:    "child_env is empty",
		},
		{
			name:    "child_env names the secret",
			content: strings.Replace(valid, `"child_env": ["HOME", "PATH"]`, `"child_env": ["HOME", "DISPATCH_TEST_SECRET"]`, 1),
			env:     true,
			want:    "the name the secret is read from",
		},
		{
			name:    "child_env names a variable that is not set",
			content: strings.Replace(valid, `"child_env": ["HOME", "PATH"]`, `"child_env": ["HOME", "DISPATCH_TEST_ABSENT"]`, 1),
			env:     true,
			want:    `variable "DISPATCH_TEST_ABSENT" named by child_env is not set in the environment`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env {
				t.Setenv("DISPATCH_TEST_SECRET", "fixture-secret-not-a-credential")
			}
			_, err := Load(write(t, tc.content))
			if err == nil {
				t.Fatal("Load succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestLoadUnknownSecretValueIsNotInError(t *testing.T) {
	t.Setenv("DISPATCH_TEST_SECRET", "")

	_, err := Load(write(t, valid))
	if err == nil {
		t.Fatal("Load succeeded with an empty secret")
	}
	if strings.Contains(err.Error(), "DISPATCH_TEST_SECRET=") {
		t.Fatalf("error carries the secret: %q", err)
	}
}

func TestReadmeExampleIsLoadable(t *testing.T) {
	t.Setenv("DISPATCH_WEBHOOK_SECRET", "fixture-secret-not-a-credential")

	c, err := Load(write(t, readmeExample(t)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Listen != "127.0.0.1:8645" || c.Path != "/github" {
		t.Fatalf("listen/path = %q %q", c.Listen, c.Path)
	}
	if c.SecretName != "DISPATCH_WEBHOOK_SECRET" {
		t.Fatalf("secret_name = %q", c.SecretName)
	}
	want := []string{"thani-sh-mama", "thani-sh-meme", "thani-sh-mimi", "thani-sh-momo"}
	profiles := c.AgentProfiles()
	if len(profiles) != len(want) {
		t.Fatalf("agents = %v, want %v", profiles, want)
	}
	for _, login := range want {
		profile := strings.TrimPrefix(login, "thani-sh-")
		if profiles[login] != profile {
			t.Fatalf("agents[%q] = %q, want %q", login, profiles[login], profile)
		}
	}
}

// readmeExample returns the JSON block the README documents as the
// configuration.
func readmeExample(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	const open = "```json\n"
	start := strings.Index(string(data), open)
	if start < 0 {
		t.Fatal("README has no json block")
	}
	rest := string(data)[start+len(open):]
	end := strings.Index(rest, "```")
	if end < 0 {
		t.Fatal("README's json block is not closed")
	}
	return rest[:end]
}
