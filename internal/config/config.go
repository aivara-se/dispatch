// Package config loads the committed configuration file: the behaviour the
// service runs on, and the names of the secrets it needs.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Agent is one agent of the fleet: the Hermes profile its run uses, and the
// GitHub login it works as.
type Agent struct {
	Profile string `json:"profile"`
	Login   string `json:"login"`
}

// Route is one inbound source — the organisation webhook, or one repository —
// and the name of the environment variable its secret lives in.
type Route struct {
	Name      string `json:"name"`
	SecretEnv string `json:"secretEnv"`
}

// Config is the whole committed configuration: where the service listens, what
// it accepts, which project is the board, and which agents and routes exist.
type Config struct {
	Addr    string   `json:"addr"`
	Path    string   `json:"path"`
	Repos   []string `json:"repos"`
	Events  []string `json:"events"`
	Owner   string   `json:"owner"`
	Project int      `json:"project"`
	Agents  []Agent  `json:"agents"`
	Routes  []Route  `json:"routes"`
}

// Load reads the configuration file at path. An unknown field is refused — a
// committed configuration with a typo in it stops the process at boot rather
// than being read as a smaller configuration than it is — and so is a
// configuration the service cannot boot with.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		if err == io.EOF {
			return Config{}, fmt.Errorf("decode config: no JSON object")
		}
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if decoder.More() {
		return Config{}, fmt.Errorf("decode config: unexpected data after the object")
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate refuses the configurations the service cannot boot with: nowhere to
// listen, no path to serve, no board to read, and names that would collide
// where the configuration is consumed.
func (c Config) validate() error {
	if c.Addr == "" {
		return fmt.Errorf("config: addr is empty")
	}
	if c.Path == "" {
		return fmt.Errorf("config: path is empty")
	}
	if !strings.HasPrefix(c.Path, "/") {
		return fmt.Errorf("config: path %q has no leading slash", c.Path)
	}
	if c.Owner == "" {
		return fmt.Errorf("config: owner is empty")
	}
	if c.Project <= 0 {
		return fmt.Errorf("config: project %d is not a project number", c.Project)
	}
	names := make(map[string]bool, len(c.Routes))
	for i, route := range c.Routes {
		if route.Name == "" {
			return fmt.Errorf("config: route %d has an empty name", i)
		}
		if route.SecretEnv == "" {
			return fmt.Errorf("config: route %s has no secret name", route.Name)
		}
		if names[route.Name] {
			return fmt.Errorf("config: route name %q appears twice", route.Name)
		}
		names[route.Name] = true
	}
	profiles := make(map[string]bool, len(c.Agents))
	logins := make(map[string]bool, len(c.Agents))
	for i, agent := range c.Agents {
		if agent.Profile == "" {
			return fmt.Errorf("config: agent %d has an empty profile", i)
		}
		if agent.Login == "" {
			return fmt.Errorf("config: agent %s has no login", agent.Profile)
		}
		if profiles[agent.Profile] {
			return fmt.Errorf("config: profile %q appears twice", agent.Profile)
		}
		if logins[agent.Login] {
			return fmt.Errorf("config: login %q appears twice", agent.Login)
		}
		profiles[agent.Profile] = true
		logins[agent.Login] = true
	}
	return nil
}

// Secrets resolves every route's secret through lookup, keyed by the route's
// name. A reference that resolves to nothing is an error: the process stops at
// boot rather than at the first delivery.
func (c Config) Secrets(lookup func(string) ([]byte, bool)) (map[string][]byte, error) {
	secrets := make(map[string][]byte, len(c.Routes))
	for _, route := range c.Routes {
		value, ok := lookup(route.SecretEnv)
		if !ok || len(value) == 0 {
			return nil, fmt.Errorf("route %s: secret %s resolves to nothing", route.Name, route.SecretEnv)
		}
		secrets[route.Name] = value
	}
	return secrets, nil
}
