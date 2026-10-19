// Package config loads and validates the one document Dispatch is configured by.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// Agent is one GitHub login and the Hermes profile a wake for it runs as.
type Agent struct {
	Login   string `json:"login"`
	Profile string `json:"profile"`
}

// Config is the committed document, with the secret it names resolved from the
// environment at load time.
type Config struct {
	Listen     string  `json:"listen"`
	Path       string  `json:"path"`
	SecretName string  `json:"secret_name"`
	Hermes     string  `json:"hermes"`
	Agents     []Agent `json:"agents"`

	Secret []byte `json:"-"`
}

// Load reads the document at path, refuses anything it does not recognise, and
// resolves the secret it names. The secret's value is never part of an error.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open configuration: %w", err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()

	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("read configuration %s: %w", path, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read configuration %s: trailing data after the document", path)
	}

	if err := c.validate(); err != nil {
		return nil, err
	}

	secret, ok := os.LookupEnv(c.SecretName)
	if !ok || secret == "" {
		return nil, fmt.Errorf("secret %q named by secret_name is not set in the environment", c.SecretName)
	}
	c.Secret = []byte(secret)

	return &c, nil
}

// AgentProfiles maps each configured GitHub login to its profile.
func (c *Config) AgentProfiles() map[string]string {
	profiles := make(map[string]string, len(c.Agents))
	for _, a := range c.Agents {
		profiles[a.Login] = a.Profile
	}
	return profiles
}

func (c *Config) validate() error {
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("listen %q is not a host:port address", c.Listen)
	}
	if !strings.HasPrefix(c.Path, "/") {
		return fmt.Errorf("path %q must start with /", c.Path)
	}
	if c.SecretName == "" {
		return errors.New("secret_name is empty")
	}
	if !filepath.IsAbs(c.Hermes) {
		return fmt.Errorf("hermes %q must be an absolute path", c.Hermes)
	}
	if len(c.Agents) == 0 {
		return errors.New("agents is empty")
	}

	seen := make(map[string]bool, len(c.Agents))
	for i, a := range c.Agents {
		if a.Login == "" {
			return fmt.Errorf("agents[%d].login is empty", i)
		}
		if a.Profile == "" {
			return fmt.Errorf("agents[%d].profile is empty", i)
		}
		if seen[a.Login] {
			return fmt.Errorf("agents[%d].login %q is listed twice", i, a.Login)
		}
		seen[a.Login] = true
	}

	return nil
}
