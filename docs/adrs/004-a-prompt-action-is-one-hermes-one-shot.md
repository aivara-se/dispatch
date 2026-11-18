# 004 A prompt action is one Hermes one-shot

## Context

Agents are Hermes profiles that hold their own credentials and sessions. A task may run several actions; only the prompt action reaches an agent. v1 launched runs and forgot the child, so it recorded no output, cost or proof. v2 must leave the agent free and still record each action.

## Decision

- A prompt action is `{ "type": "prompt", "agent": "<name>", "text": "<prompt>" }`; the agent names a profile in `dispatch.json`.
- It runs `hermes --profile <profile> chat -q "<text>" -Q --source tool -c "<job key>" --create-if-missing`.
- The job key is the session title; a later prompt in the same job and profile resumes its thread.
- stdout is the action's output; stderr ends with `session_id: <id>`.
- After exit the engine reads that session in the profile's `state.db` for the tool calls. A missing session leaves them null.
- The engine holds the pid and kills the running action when the task's `limit.time` expires. A non-zero exit fails the task; the condition still decides.
- The engine passes no credentials and sends nothing mid-task.
- `hermes` in `dispatch.json` is an absolute path.

## Consequences

The adapter depends on the Hermes CLI and the session store, so a change there breaks it. Cost and tool calls are read after the fact, never streamed. A prompt action can be killed, not steered. Script and action types never reach an agent; the engine runs them.
