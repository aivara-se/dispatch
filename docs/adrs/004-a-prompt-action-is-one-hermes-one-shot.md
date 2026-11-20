# 004 A prompt action is one Hermes one-shot

## Context

Agents are Hermes profiles that hold their own credentials and sessions. A task runs several actions, and only the prompt action reaches an agent.

## Decision

- A prompt action is `{ "type": "prompt", "agent": "<name>", "text": "<prompt>" }`; the agent names a profile in `dispatch.json`.
- It runs `hermes --profile <profile> chat -q "<text>" -Q --source tool -c "<job key>" --create-if-missing`, with `hermes` an absolute path.
- The job key is the session title, so a later prompt in the same job and profile resumes its thread.
- stdout is the action's output; stderr ends with `session_id: <id>`. After exit the engine reads that session in the profile's `state.db` for the tool calls.
- The engine holds the pid and kills the action when the task's `limit.time` expires. A non-zero exit fails the task; the condition still decides.
- The engine passes no credentials and sends nothing mid-task.

## Consequences

The adapter depends on the Hermes CLI and the session store, so a change there breaks it. Cost and tool calls are read after the fact, never streamed. A prompt action can be killed, not steered.
