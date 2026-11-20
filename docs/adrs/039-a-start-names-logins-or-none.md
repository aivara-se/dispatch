# 039 A start names logins or none

## Context

`038` makes a start that names a login write one job per agent, and requires `{agent}` in the key of such a flow. `_start_.next` is an ordered list and the first condition that holds wins, so a flow may name a login condition and a pull request condition together. On `opened` with reviewers both hold, the pull request condition comes first, and the job takes no agent.

Measured on `21add36`: `{"pr.created": "review", "pr.review_requested": "review"}` with key `{n}-{agent}` loaded. An `opened` delivery naming two reviewers then wrote one job, key `212-`, `agent ""`, and started `review`; its `{agent}` prompt meets `agent "" is not configured` and the job stops blocked. Two reviewers were requested and none woke. The loader admits a configuration whose first delivery fails, which is the class the boot check exists to refuse.

## Decision

- `_start_.next` names login conditions or none of them. The loader refuses a flow that names both, under `tasks._start_.next`.
- The refusal is the flow's, not the delivery's: the loader cannot know which condition a later delivery proves, and a flow that loads must run.

## Consequences

- A flow cannot start one branch per pull request and another per agent. That is two flows, which is also what a task that must run once per pull request needs (`038`).
- A deployment cut from a flow that mixes them stops at boot with the file and the key, instead of stopping at its first delivery with a job that woke nobody.
- No earlier clause changes: `029`, `033` and `035` keep their condition lists, and `038`'s key rule stands unchanged for the flows this one refuses.
