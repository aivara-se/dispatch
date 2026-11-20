# Configuration

This directory is an example; a deployment brings its own, flows included.

`dispatch.json`

- `listen` — the address the server binds.
- `path` — the URL path GitHub posts to.
- `secret_name` — the environment variable holding the webhook secret; the file never holds it.
- `app_id` — the GitHub App's numeric id.
- `key_path` — the path to the App's private key; the engine reads it at boot and never logs it.
- `repos` — the repositories the engine serves: `owner/*` or `owner/name`.
- `hermes` — the absolute path to the Hermes binary.
- `agents` — a name, the GitHub login that wakes it, and the Hermes profile that runs it. `login` is required and held by one agent.

`flows/<name>.json`

- The file name is the flow name. The flows here are examples; a deployment brings its own.
- `key` — the job's last path segment; it must contain `{n}`, the pull request number, so two pull requests cannot share one.
- `tasks` — `_start_` and one entry per task.
- `_start_.next` — the delivery that starts a job. It names login conditions or pull request conditions, never both; the loader refuses a mix.
- A task has `actions`, a `limit`, and `next`. `limit.turn` is how many times the task may run in one job; `limit.time` is the task's whole budget, in seconds, and the engine kills a running action when it runs out. `_start_` is special: it has only `next`.
- `next` maps a condition to a task, or to `_done_`, which ends the job as a success.
- A flow whose `_start_.next` names a login condition carries `{agent}`, the agent the delivery named, in its `key`; its prompt actions may name `{agent}` as their agent. The loader refuses a login flow without `{agent}` in its key, and refuses `{agent}` in a flow whose start names no login.
- A prompt action's `text` uses `{pr.url}`, the pull request's URL.

## Actions

`actions` run in order, once each, with no check between them. Every one must exit zero; when one does not, the task fails and the job stops.

- `{ "type": "prompt", "agent": "<name>", "text": "<prompt>" }` — one Hermes one-shot in that agent's profile.
- `{ "type": "script", "path": "<file>", "args": ["..."] }` — an executable the engine runs. The path is relative to this `config/` directory.
- `{ "type": "action", "name": "label", "value": "<label>" }` — an engine action. `label` is the only one now; the engine uses it to write `blocked` when a job stops.

A job is `{flow}/{owner}/{repo}/{key}`, so the review flow's job reads `review/aivara-se/dispatch/212-momo`. The condition set is ADR [`029`](../docs/adrs/029-the-engine-reads-a-closed-condition-set.md), widened to seven by [`038`](../docs/adrs/038-a-delivery-wakes-the-agent-its-login-names.md): `pr.review_requested` and `pr.assigned` are proved by a delivery alone.
