package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// PlaceholderAgent stands in a flow's key or a prompt action's agent for the
// agent the delivery named. PlaceholderPR stands in a prompt's text for the
// pull request's URL.
const (
	PlaceholderAgent = "{agent}"
	PlaceholderPR    = "{pr.url}"
)

// The task names a flow does not declare: where a flow starts, and the end of
// it. _start_ carries a next map and no actions; _done_ is a target and no
// task.
const (
	TaskStart = "_start_"
	TaskDone  = "_done_"
)

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

// Action is one step a task runs. The type decides which of the fields
// apply: an agent and a text for a prompt, a path and args for a script, a
// name and a value for a named action.
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

// KeyFor builds the key of a job a delivery starts: the flow's key with the
// pull request number and the agent in place of their placeholders.
func (f Flow) KeyFor(number int, agent string) string {
	key := strings.ReplaceAll(f.Key, "{n}", strconv.Itoa(number))
	return strings.ReplaceAll(key, PlaceholderAgent, agent)
}
