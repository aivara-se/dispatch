package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aivara-se/dispatch/internal/config"
	"github.com/aivara-se/dispatch/internal/record"
)

// result is what one action produced.
type result struct {
	output string
	exit   int
	tools  string
}

// actionFunc runs one action and returns what it produced. A handler returns
// an error only when the action could not run at all; a refused action is a
// non-zero exit with its reason as the output.
type actionFunc func(ctx context.Context, job record.Job, action config.Action) (result, error)

// perform runs the task's actions in order, resuming after the last finished
// one. It returns the merged tool calls, and whether an action failed.
func (r *Runner) perform(ctx context.Context, job record.Job, task config.Task, current record.Task) (string, bool, error) {
	taskCtx, cancel := context.WithTimeout(ctx, time.Duration(task.Limit.Time)*time.Second)
	defer cancel()

	tools := current.Tools
	for i, action := range task.Actions {
		if done := actionAt(current.Actions, i); done != nil && (done.Exit != nil || done.Skipped) {
			continue
		}
		if ctx.Err() != nil {
			return tools, false, ctx.Err()
		}
		actionID, err := r.store.BeginAction(ctx, current.ID, i, recorded(action, job))
		if err != nil {
			return tools, false, err
		}
		ran, err := r.act(taskCtx, job, action)
		if ctx.Err() != nil {
			return tools, false, ctx.Err()
		}
		if taskCtx.Err() != nil {
			r.store.FinishAction(context.Background(), actionID, ran.output, -1)
			return tools, true, nil
		}
		if err != nil {
			r.store.FinishAction(context.Background(), actionID, ran.output, 1)
			return tools, true, nil
		}
		if ran.exit != 0 && ran.output == "" {
			ran.output = "exit " + strconv.Itoa(ran.exit)
		}
		if err := r.store.FinishAction(ctx, actionID, ran.output, ran.exit); err != nil {
			return tools, false, err
		}
		tools = merge(tools, ran.tools)
		if ran.exit != 0 {
			r.skip(job, task, current, i+1)
			return tools, true, nil
		}
	}
	return tools, false, nil
}

// skip records the actions a failed one left unrun, so the task shows what
// did not happen.
func (r *Runner) skip(job record.Job, task config.Task, current record.Task, from int) {
	for i := from; i < len(task.Actions); i++ {
		r.store.SkipAction(context.Background(), current.ID, i, recorded(task.Actions[i], job))
	}
}

func (r *Runner) act(ctx context.Context, job record.Job, action config.Action) (result, error) {
	run, ok := r.handlers[action.Type]
	if !ok {
		return result{exit: 1}, fmt.Errorf("unknown action type %q", action.Type)
	}
	return run(ctx, job, action)
}

func (r *Runner) prompt(ctx context.Context, job record.Job, action config.Action) (result, error) {
	agent := agentOf(action, job)
	profile, ok := r.agents[agent]
	if !ok {
		return result{exit: 1}, fmt.Errorf("agent %q is not configured", agent)
	}
	text := strings.ReplaceAll(action.Text, config.PlaceholderPR, job.Link)
	ran, err := r.launch.Run(ctx, profile, text, job.Identity())
	return result{output: ran.Output, exit: ran.Exit, tools: ran.Tools}, err
}

func (r *Runner) script(ctx context.Context, job record.Job, action config.Action) (result, error) {
	command := exec.CommandContext(ctx, filepath.Join(r.configDir, action.Path), action.Args...)
	output, err := command.CombinedOutput()
	if err == nil {
		return result{output: string(output)}, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return result{output: string(output), exit: exit.ExitCode()}, nil
	}
	if ctx.Err() != nil {
		return result{output: string(output), exit: -1}, ctx.Err()
	}
	return result{output: string(output), exit: 1}, nil
}

func (r *Runner) named(ctx context.Context, job record.Job, action config.Action) (result, error) {
	if action.Name != config.ActionLabel {
		return result{exit: 1}, fmt.Errorf("unknown action %q", action.Name)
	}
	if err := r.labels.Block(ctx, job.Owner, job.Repo, job.Number); err != nil {
		return result{output: err.Error(), exit: 1}, nil
	}
	return result{}, nil
}

// agentOf resolves the agent an action names: the placeholder becomes the
// agent whose login started this job.
func agentOf(action config.Action, job record.Job) string {
	if action.Agent == config.PlaceholderAgent {
		return job.Agent
	}
	return action.Agent
}

// recorded is the row an action writes before it runs.
func recorded(action config.Action, job record.Job) record.Action {
	return record.Action{
		Type: action.Type, Agent: agentOf(action, job), Text: action.Text,
		Path: action.Path, Args: action.Args, Name: action.Name, Value: action.Value,
	}
}

// actionAt returns the recorded row of one ordinal, when the task ran it.
func actionAt(actions []record.Action, ordinal int) *record.Action {
	if ordinal < len(actions) {
		return &actions[ordinal]
	}
	return nil
}

// merge joins two JSON arrays of tool calls.
func merge(current, next string) string {
	if next == "" {
		return current
	}
	if current == "" {
		return next
	}
	var a, b []json.RawMessage
	if json.Unmarshal([]byte(current), &a) != nil || json.Unmarshal([]byte(next), &b) != nil {
		return next
	}
	body, err := json.Marshal(append(a, b...))
	if err != nil {
		return next
	}
	return string(body)
}
