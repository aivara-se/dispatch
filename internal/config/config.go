// Package config loads the engine's configuration: one dispatch.json and the
// flows beside it. It refuses unknown fields and values outside the closed
// condition set, so a running engine never meets them.
package config

import (
	"path/filepath"
	"sort"
	"strings"
)

// Agent is one configured Hermes profile and the GitHub login that wakes it.
type Agent struct {
	Profile string `json:"profile"`
	Login   string `json:"login"`
}

// AgentFor returns the agent a login names.
func AgentFor(agents map[string]Agent, login string) (string, bool) {
	for name, agent := range agents {
		if strings.EqualFold(agent.Login, login) {
			return name, true
		}
	}
	return "", false
}

// Config is dispatch.json: the repositories served, the Hermes binary, the
// agents, the listening address and the GitHub App.
type Config struct {
	Repos      []string         `json:"repos"`
	Hermes     string           `json:"hermes"`
	Agents     map[string]Agent `json:"agents"`
	Listen     string           `json:"listen"`
	Path       string           `json:"path"`
	SecretName string           `json:"secret_name"`
	AppID      int64            `json:"app_id"`
	KeyPath    string           `json:"key_path"`
}

// Load reads dispatch.json and every flows/*.json below dir.
func Load(dir string) (*Config, []Flow, error) {
	file := filepath.Join(dir, "dispatch.json")
	var cfg Config
	if err := decodeFile(file, &cfg); err != nil {
		return nil, nil, err
	}
	if err := cfg.validate(file); err != nil {
		return nil, nil, err
	}

	files, err := filepath.Glob(filepath.Join(dir, "flows", "*.json"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(files)
	flows := make([]Flow, 0, len(files))
	for _, path := range files {
		flow, err := loadFlow(path, &cfg)
		if err != nil {
			return nil, nil, err
		}
		flows = append(flows, flow)
	}
	return &cfg, flows, nil
}
