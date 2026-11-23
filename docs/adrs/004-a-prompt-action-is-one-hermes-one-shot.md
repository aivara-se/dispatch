# 004 - A prompt action is one Hermes one-shot

## Context

Agents are Hermes profiles that hold their own credentials and sessions. A task runs several actions, and only the prompt action reaches an agent. The engine must start one prompt without holding the agent's credentials and without staying in the thread to steer it.

The output and the tool calls are the evidence a later reader checks, and both must land in the record after the action ends.

## Decision

- A prompt action is `{ "type": "prompt", "agent": "<name>", "text": "<prompt>" }`; the agent names a profile in the engine's configuration.
- It runs `hermes --profile <profile> chat -q "<text>" -Q --source tool -c "<job key>" --create-if-missing`, with the Hermes binary an absolute path.
- The job key is the session title, so a later prompt in the same job and profile resumes its thread.
- stdout is the action's output; stderr ends with the session id. After exit the engine reads that session in the profile's store for the tool calls.
- The engine holds the process id and kills the action when the task's time limit expires. A non-zero exit fails the task; the condition still decides.
- The engine passes no credentials and sends nothing mid-task.

## Consequences

The adapter depends on the Hermes CLI and its session store, so a change there breaks it. Cost and tool calls are read after the fact, never streamed. A prompt action can be killed, not steered. A resumed thread carries the earlier turns, so a task that prompts twice pays for both.
