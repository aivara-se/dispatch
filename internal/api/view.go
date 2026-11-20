package api

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/aivara-se/dispatch/internal/config"
	"github.com/aivara-se/dispatch/internal/record"
)

// jobCard is one job as the job lists show it.
type jobCard struct {
	Flow    string  `json:"flow"`
	Owner   string  `json:"owner"`
	Repo    string  `json:"repo"`
	Key     string  `json:"key"`
	Agent   string  `json:"agent,omitempty"`
	Ref     string  `json:"ref"`
	Title   string  `json:"title"`
	State   string  `json:"state"`
	Verdict string  `json:"verdict"`
	Cause   string  `json:"cause"`
	Started string  `json:"started"`
	Alert   *string `json:"alert"`
	Link    string  `json:"link"`
}

type jobDetail struct {
	jobCard
	Tasks []taskView `json:"tasks"`
}

type taskView struct {
	N        int             `json:"n"`
	Short    string          `json:"short"`
	At       string          `json:"at"`
	Actions  []actionView    `json:"actions"`
	PC       json.RawMessage `json:"pc"`
	Evidence json.RawMessage `json:"evidence"`
	Tools    json.RawMessage `json:"tools"`
}

type actionView struct {
	Type    string   `json:"type"`
	Output  *string  `json:"output"`
	Exit    *int     `json:"exit"`
	Skipped bool     `json:"skipped,omitempty"`
	Agent   string   `json:"agent,omitempty"`
	Text    string   `json:"text,omitempty"`
	Path    string   `json:"path,omitempty"`
	Args    []string `json:"args,omitempty"`
	Name    string   `json:"name,omitempty"`
	Value   string   `json:"value,omitempty"`
}

func cardOf(job record.Job) jobCard {
	card := jobCard{
		Flow:    job.Flow,
		Owner:   job.Owner,
		Repo:    job.Repo,
		Key:     job.Key,
		Agent:   job.Agent,
		Ref:     fmt.Sprintf("%s/%s#%d", job.Owner, job.Repo, job.Number),
		Title:   job.Title,
		State:   job.State,
		Verdict: job.Verdict,
		Cause:   job.UpdatedAt.Format(time.RFC3339),
		Started: job.CreatedAt.Format(time.RFC3339),
		Link:    job.Link,
	}
	if job.Alert != "" {
		alert := job.Alert
		card.Alert = &alert
	}
	return card
}

// taskOf hides the fields an action of another type leaves empty, so a client
// reads only what its type carries.
func taskOf(task record.Task) taskView {
	view := taskView{
		N:        task.N,
		Short:    task.Short,
		At:       task.At,
		Actions:  make([]actionView, 0, len(task.Actions)),
		PC:       raw(task.PC),
		Evidence: raw(task.Evidence),
		Tools:    raw(task.Tools),
	}
	for _, action := range task.Actions {
		view.Actions = append(view.Actions, actionOf(action))
	}
	return view
}

func actionOf(action record.Action) actionView {
	item := actionView{Type: action.Type, Exit: action.Exit, Skipped: action.Skipped}
	if action.Output != "" {
		output := action.Output
		item.Output = &output
	}
	switch action.Type {
	case config.ActionPrompt:
		item.Agent = action.Agent
		item.Text = action.Text
	case config.ActionScript:
		item.Path = action.Path
		item.Args = action.Args
	case config.ActionNamed:
		item.Name = action.Name
		item.Value = action.Value
	}
	return item
}

func raw(text string) json.RawMessage {
	if text == "" || !json.Valid([]byte(text)) {
		return nil
	}
	return json.RawMessage(text)
}
