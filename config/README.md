# Configuration

This directory is an example; a deployment brings its own. The `review` flow here is an example, not part of Dispatch.

`dispatch.json`

- `listen` — the address the server binds.
- `path` — the URL path GitHub posts to.
- `secret_name` — the environment variable holding the webhook secret; the file never holds it.
- `app_id` — the GitHub App's numeric id.
- `key_path` — the path to the App's private key; the engine reads it at boot and never logs it.
- `repos` — the repositories the engine serves: `owner/*` or `owner/name`.
- `hermes` — the absolute path to the Hermes binary.
- `agents` — a name and the Hermes profile that runs it.

`flows/<name>.json`

- The file name is the flow name. `review` is an example; flows belong to the deployment, not to Dispatch.
- `key` — the job's last path segment; it must contain `{n}`, the pull request number, so two pull requests cannot share one.
- `tasks` — `_start_` and one entry per task.
- `_start_.next` — the delivery that starts a job.
- A task has `actions`, a `limit`, and `next`. `limit.turn` is how many times the task may run in one job; `limit.time` is the task's whole budget, in seconds, and the engine kills a running action when it runs out. `_start_` is special: it has only `next`.
- `next` maps a condition to a task, or to `_done_`, which ends the job as a success.
- A prompt action's `text` uses `{pr.url}`, the pull request's URL.

## Actions

`actions` run in order, once each, with no check between them. Every one must exit zero; when one does not, the task fails and the job stops.

- `{ "type": "prompt", "agent": "<name>", "text": "<prompt>" }` — one Hermes one-shot in that agent's profile.
- `{ "type": "script", "path": "<file>", "args": ["..."] }` — an executable the engine runs. The path is relative to this `config/` directory.
- `{ "type": "action", "name": "label", "value": "<label>" }` — an engine action. `label` is the only one now; the engine uses it to write `blocked` when a job stops.

A job is `{flow}/{owner}/{repo}/{key}`, so the review flow's job reads `review/aivara-se/dispatch/212`. The conditions are in ADR [`002`](../docs/adrs/002-the-engine-reads-a-closed-condition-set.md).
