package config

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

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
	if err := c.validateAgents(file); err != nil {
		return err
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

func (c *Config) validateAgents(file string) error {
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
	return nil
}

// flowFile is a flow file, its tasks still undecoded.
type flowFile struct {
	Key   string                     `json:"key"`
	Tasks map[string]json.RawMessage `json:"tasks"`
}

func loadFlow(path string, cfg *Config) (Flow, error) {
	var head flowFile
	if err := decodeFile(path, &head); err != nil {
		return Flow{}, err
	}
	if err := head.validateKey(path); err != nil {
		return Flow{}, err
	}

	start, ok := head.Tasks[TaskStart]
	if !ok {
		return Flow{}, keyError(path, "tasks."+TaskStart, "required")
	}
	var startTask struct {
		Next Next `json:"next"`
	}
	if err := decodeRaw(path, "tasks._start_", start, &startTask); err != nil {
		return Flow{}, err
	}

	names, err := head.taskNames(path)
	if err != nil {
		return Flow{}, err
	}
	known := known(names)
	flow := Flow{Name: flowName(path), Key: head.Key, Start: startTask.Next, Tasks: map[string]Task{}}
	if err := validateNext(path, "tasks._start_.next", flow.Start, known); err != nil {
		return Flow{}, err
	}
	perLogin, err := startKind(path, flow, head.Key)
	if err != nil {
		return Flow{}, err
	}
	for _, name := range names {
		task, err := decodeTask(path, cfg, name, head.Tasks[name], perLogin, known)
		if err != nil {
			return Flow{}, err
		}
		flow.Tasks[name] = task
	}
	return flow, nil
}

// validateKey refuses a key that two pull requests, or two agents, could
// share, or a key that names the agent its start never sets.
func (f flowFile) validateKey(path string) error {
	if f.Key == "" {
		return keyError(path, "key", "required")
	}
	if f.Key == "." || f.Key == ".." || strings.Contains(f.Key, "/") {
		return keyError(path, "key", fmt.Sprintf("%q must be one path segment", f.Key))
	}
	if !strings.Contains(f.Key, "{n}") {
		return keyError(path, "key", "must contain {n} so two pull requests cannot share a key")
	}
	return nil
}

// taskNames returns every task name of the flow, sorted, refusing the name
// the engine keeps for a finished job.
func (f flowFile) taskNames(path string) ([]string, error) {
	names := make([]string, 0, len(f.Tasks))
	for name := range f.Tasks {
		if name == TaskStart {
			continue
		}
		if name == TaskDone {
			return nil, keyError(path, "tasks."+TaskDone, "reserved name")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// startKind reads the start map and reports whether it names logins. A start
// names logins or none (docs/adrs/002): on a delivery that proves both, the
// first key would start a job with no agent in a flow written for one, and
// the agent would never wake.
func startKind(path string, flow Flow, key string) (bool, error) {
	perLogin, perRequest := false, false
	for _, condition := range flow.Start {
		if !Proven(condition.Key) {
			return false, keyError(path, "tasks._start_.next."+condition.Key,
				"no delivery proves this condition")
		}
		if PerLogin(condition.Key) {
			perLogin = true
		} else {
			perRequest = true
		}
	}
	if perLogin && perRequest {
		return false, keyError(path, "tasks._start_.next",
			"names a login condition and a pull request condition")
	}
	namesAgent := strings.Contains(key, PlaceholderAgent)
	if perLogin && !namesAgent {
		return false, keyError(path, "key", "must contain "+PlaceholderAgent+": its start names a login")
	}
	if namesAgent && !perLogin {
		return false, keyError(path, "key", PlaceholderAgent+" names no login in tasks._start_.next")
	}
	return perLogin, nil
}

func decodeTask(path string, cfg *Config, name string, raw json.RawMessage, perLogin bool, known map[string]bool) (Task, error) {
	var tf struct {
		Actions []Action `json:"actions"`
		Limit   *Limit   `json:"limit"`
		Next    Next     `json:"next"`
	}
	if err := decodeRaw(path, "tasks."+name, raw, &tf); err != nil {
		return Task{}, err
	}
	if tf.Limit == nil {
		return Task{}, keyError(path, "tasks."+name+".limit", "required")
	}
	if tf.Limit.Turn < 1 {
		return Task{}, keyError(path, "tasks."+name+".limit.turn", "must be at least 1")
	}
	if tf.Limit.Time < 1 {
		return Task{}, keyError(path, "tasks."+name+".limit.time", "must be at least 1")
	}
	for i, action := range tf.Actions {
		key := "tasks." + name + fmt.Sprintf(".actions[%d]", i)
		if err := action.validate(path, key, cfg); err != nil {
			return Task{}, err
		}
		if action.Agent == PlaceholderAgent && !perLogin {
			return Task{}, keyError(path, key+".agent",
				PlaceholderAgent+" names no login in tasks._start_.next")
		}
	}
	if err := validateNext(path, "tasks."+name+".next", tf.Next, known); err != nil {
		return Task{}, err
	}
	return Task{Actions: tf.Actions, Limit: *tf.Limit, Next: tf.Next}, nil
}

func (a Action) validate(path, key string, cfg *Config) error {
	switch a.Type {
	case ActionPrompt:
		if a.Agent == "" {
			return keyError(path, key+".agent", "required")
		}
		if _, ok := cfg.Agents[a.Agent]; !ok && a.Agent != PlaceholderAgent {
			return keyError(path, key+".agent", fmt.Sprintf("unknown agent %q", a.Agent))
		}
		if a.Text == "" {
			return keyError(path, key+".text", "required")
		}
	case ActionScript:
		if a.Path == "" {
			return keyError(path, key+".path", "required")
		}
	case ActionNamed:
		if a.Name != ActionLabel {
			return keyError(path, key+".name", fmt.Sprintf("unknown action %q", a.Name))
		}
		if a.Value == "" {
			return keyError(path, key+".value", "required")
		}
		if a.Value != LabelBlocked {
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
		if c.Target != TaskDone && !tasks[c.Target] {
			return keyError(path, key+"."+c.Key, fmt.Sprintf("unknown task %q", c.Target))
		}
	}
	return nil
}

func known(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

func flowName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
