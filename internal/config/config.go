// Package config loads the engine's configuration: one dispatch.json and the
// flows beside it. It refuses unknown fields and values outside the closed
// condition set, so a running engine never meets them.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The closed condition set a flow may name.
const (
	ConditionCreated         = "pr.created"
	ConditionApproved        = "pr.approved"
	ConditionRejected        = "pr.rejected"
	ConditionUpdated         = "pr.updated"
	ConditionMerged          = "pr.merged"
	ConditionReviewRequested = "pr.review_requested"
	ConditionAssigned        = "pr.assigned"
)

// PlaceholderAgent stands in a flow's key or a prompt action's agent for the
// agent the delivery named.
const PlaceholderAgent = "{agent}"

var conditions = map[string]bool{
	ConditionCreated:         true,
	ConditionApproved:        true,
	ConditionRejected:        true,
	ConditionUpdated:         true,
	ConditionMerged:          true,
	ConditionReviewRequested: true,
	ConditionAssigned:        true,
}

// starts are the conditions a delivery proves, so they are the only keys
// _start_.next may name. pr.updated and pr.merged are read after a task.
var starts = map[string]bool{
	ConditionCreated:         true,
	ConditionApproved:        true,
	ConditionRejected:        true,
	ConditionReviewRequested: true,
	ConditionAssigned:        true,
}

// PerLogin reports whether a start condition starts one job per login an
// agent holds, rather than one job per pull request.
func PerLogin(key string) bool {
	return key == ConditionReviewRequested || key == ConditionAssigned
}

// ValidCondition reports whether name is one of the five conditions.
func ValidCondition(name string) bool {
	return conditions[name]
}

// Error names the file and the key that failed the load.
type Error struct {
	File string
	Key  string
	Msg  string
}

// Error renders the file, the key and the reason on one line.
func (e *Error) Error() string {
	if e.Key == "" {
		return e.File + ": " + e.Msg
	}
	return e.File + ": " + e.Key + ": " + e.Msg
}

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

// Condition is one key of a next map, kept in the order the file wrote it.
type Condition struct {
	Key    string
	Target string
}

// Next is a task's next map, in file order; the first key that holds wins.
type Next []Condition

// UnmarshalJSON reads the object pair by pair so order survives the decode.
func (n *Next) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('{') {
		return errors.New("must be an object")
	}
	var out Next
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := keyTok.(string)
		if !ok {
			return errors.New("condition must be a string")
		}
		var target string
		if err := dec.Decode(&target); err != nil {
			return err
		}
		out = append(out, Condition{Key: key, Target: target})
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	*n = out
	return nil
}

// Action is one step a task runs.
type Action struct {
	Type  string   `json:"type"`
	Agent string   `json:"agent,omitempty"`
	Text  string   `json:"text,omitempty"`
	Path  string   `json:"path,omitempty"`
	Args  []string `json:"args,omitempty"`
	Name  string   `json:"name,omitempty"`
	Value string   `json:"value,omitempty"`
}

// Limit is a task's budget: runs in one job, and seconds per run.
type Limit struct {
	Turn int `json:"turn"`
	Time int `json:"time"`
}

// Task is one named step in a flow.
type Task struct {
	Actions []Action
	Limit   Limit
	Next    Next
}

// Flow is one flows/<name>.json file.
type Flow struct {
	Name  string
	Key   string
	Start Next
	Tasks map[string]Task
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

func (c *Config) validate(file string) error {
	if c.Listen == "" {
		return keyError(file, "listen", "required")
	}
	if !strings.HasPrefix(c.Path, "/") {
		return keyError(file, "path", "must start with a slash")
	}
	if c.SecretName == "" {
		return keyError(file, "secret_name", "required")
	}
	if c.AppID <= 0 {
		return keyError(file, "app_id", "required")
	}
	if c.KeyPath == "" {
		return keyError(file, "key_path", "required")
	}
	if !filepath.IsAbs(c.Hermes) {
		return keyError(file, "hermes", "must be an absolute path")
	}
	names := make([]string, 0, len(c.Agents))
	for name := range c.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	held := make(map[string]string, len(names))
	for _, name := range names {
		if name == "" {
			return keyError(file, "agents", "empty agent name")
		}
		agent := c.Agents[name]
		if agent.Profile == "" {
			return keyError(file, "agents."+name+".profile", "required")
		}
		if agent.Login == "" {
			return keyError(file, "agents."+name+".login", "required")
		}
		lower := strings.ToLower(agent.Login)
		if other, ok := held[lower]; ok {
			return keyError(file, "agents."+name+".login",
				fmt.Sprintf("duplicate login, also held by %q", other))
		}
		held[lower] = name
	}
	for i, repo := range c.Repos {
		owner, name, ok := strings.Cut(repo, "/")
		if !ok || owner == "" || name == "" {
			return keyError(file, fmt.Sprintf("repos[%d]", i), "must be owner/name or owner/*")
		}
		if strings.ContainsAny(owner+name, "*?") && name != "*" {
			return keyError(file, fmt.Sprintf("repos[%d]", i), "must be owner/name or owner/*")
		}
	}
	return nil
}

func loadFlow(path string, cfg *Config) (Flow, error) {
	var head struct {
		Key   string                     `json:"key"`
		Tasks map[string]json.RawMessage `json:"tasks"`
	}
	if err := decodeFile(path, &head); err != nil {
		return Flow{}, err
	}

	if head.Key == "" {
		return Flow{}, keyError(path, "key", "required")
	}
	if head.Key == "." || head.Key == ".." || strings.Contains(head.Key, "/") {
		return Flow{}, keyError(path, "key", fmt.Sprintf("%q must be one path segment", head.Key))
	}
	if !strings.Contains(head.Key, "{n}") {
		return Flow{}, keyError(path, "key", "must contain {n} so two pull requests cannot share a key")
	}

	start, ok := head.Tasks["_start_"]
	if !ok {
		return Flow{}, keyError(path, "tasks._start_", "required")
	}
	var startTask struct {
		Next Next `json:"next"`
	}
	if err := decodeRaw(path, "tasks._start_", start, &startTask); err != nil {
		return Flow{}, err
	}

	names := make([]string, 0, len(head.Tasks))
	for name := range head.Tasks {
		if name == "_start_" {
			continue
		}
		if name == "_done_" {
			return Flow{}, keyError(path, "tasks._done_", "reserved name")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	known := make(map[string]bool, len(names))
	for _, name := range names {
		known[name] = true
	}

	flow := Flow{Name: flowName(path), Key: head.Key, Start: startTask.Next, Tasks: map[string]Task{}}
	if err := validateNext(path, "tasks._start_.next", flow.Start, known); err != nil {
		return Flow{}, err
	}
	perLogin, perRequest := false, false
	for _, c := range flow.Start {
		if !starts[c.Key] {
			return Flow{}, keyError(path, "tasks._start_.next."+c.Key, "no delivery proves this condition")
		}
		if PerLogin(c.Key) {
			perLogin = true
		} else {
			perRequest = true
		}
	}
	// A start names logins or none (docs/adrs/002). On a delivery that proves
	// both, the first key would start a job with no agent in a flow written
	// for one, and the agent would never wake.
	if perLogin && perRequest {
		return Flow{}, keyError(path, "tasks._start_.next",
			"names a login condition and a pull request condition")
	}
	// Two agents' jobs must not share a session, and the session is the key.
	namesAgent := strings.Contains(head.Key, PlaceholderAgent)
	if perLogin && !namesAgent {
		return Flow{}, keyError(path, "key", "must contain "+PlaceholderAgent+": its start names a login")
	}
	if namesAgent && !perLogin {
		return Flow{}, keyError(path, "key", PlaceholderAgent+" names no login in tasks._start_.next")
	}
	for _, name := range names {
		var tf struct {
			Actions []Action `json:"actions"`
			Limit   *Limit   `json:"limit"`
			Next    Next     `json:"next"`
		}
		if err := decodeRaw(path, "tasks."+name, head.Tasks[name], &tf); err != nil {
			return Flow{}, err
		}
		if tf.Limit == nil {
			return Flow{}, keyError(path, "tasks."+name+".limit", "required")
		}
		if tf.Limit.Turn < 1 {
			return Flow{}, keyError(path, "tasks."+name+".limit.turn", "must be at least 1")
		}
		if tf.Limit.Time < 1 {
			return Flow{}, keyError(path, "tasks."+name+".limit.time", "must be at least 1")
		}
		for i, action := range tf.Actions {
			key := "tasks." + name + fmt.Sprintf(".actions[%d]", i)
			if err := action.validate(path, key, cfg); err != nil {
				return Flow{}, err
			}
			if action.Agent == PlaceholderAgent && !perLogin {
				return Flow{}, keyError(path, key+".agent",
					PlaceholderAgent+" names no login in tasks._start_.next")
			}
		}
		if err := validateNext(path, "tasks."+name+".next", tf.Next, known); err != nil {
			return Flow{}, err
		}
		flow.Tasks[name] = Task{Actions: tf.Actions, Limit: *tf.Limit, Next: tf.Next}
	}
	return flow, nil
}

func (a Action) validate(path, key string, cfg *Config) error {
	switch a.Type {
	case "prompt":
		if a.Agent == "" {
			return keyError(path, key+".agent", "required")
		}
		if _, ok := cfg.Agents[a.Agent]; !ok && a.Agent != PlaceholderAgent {
			return keyError(path, key+".agent", fmt.Sprintf("unknown agent %q", a.Agent))
		}
		if a.Text == "" {
			return keyError(path, key+".text", "required")
		}
	case "script":
		if a.Path == "" {
			return keyError(path, key+".path", "required")
		}
	case "action":
		if a.Name != "label" {
			return keyError(path, key+".name", fmt.Sprintf("unknown action %q", a.Name))
		}
		if a.Value == "" {
			return keyError(path, key+".value", "required")
		}
		if a.Value != "blocked" {
			return keyError(path, key+".value", `the only label is "blocked"`)
		}
	default:
		return keyError(path, key+".type", fmt.Sprintf("unknown type %q", a.Type))
	}
	return nil
}

func validateNext(path, key string, next Next, tasks map[string]bool) error {
	for _, c := range next {
		if !ValidCondition(c.Key) {
			return keyError(path, key+"."+c.Key, fmt.Sprintf("unknown condition %q", c.Key))
		}
		if c.Target != "_done_" && !tasks[c.Target] {
			return keyError(path, key+"."+c.Key, fmt.Sprintf("unknown task %q", c.Target))
		}
	}
	return nil
}

func flowName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func decodeFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := decodeInto(data, v); err != nil {
		return decodeError(path, "", err)
	}
	return nil
}

func decodeRaw(path, key string, data []byte, v any) error {
	if err := decodeInto(data, v); err != nil {
		return decodeError(path, key, err)
	}
	return nil
}

// decodeError names the key that failed. An unknown field is reported at its
// own path, so the operator sees the file, the task and the field.
func decodeError(path, key string, err error) error {
	const prefix = "json: unknown field "
	msg := err.Error()
	if strings.HasPrefix(msg, prefix) {
		if field, err := strconv.Unquote(strings.TrimPrefix(msg, prefix)); err == nil {
			if key != "" {
				key += "." + field
			} else {
				key = field
			}
			return &Error{File: path, Key: key, Msg: "unknown field"}
		}
	}
	return &Error{File: path, Key: key, Msg: msg}
}

func decodeInto(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing data")
		}
		return err
	}
	return nil
}

func keyError(file, key, msg string) error {
	return &Error{File: file, Key: key, Msg: msg}
}
