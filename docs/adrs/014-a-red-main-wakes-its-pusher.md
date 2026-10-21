# 014 - A Red Main Wakes Its Pusher

## Context

`010` acts on a failing `workflow_run` only when it names a pull request, because the pull request's number is the pointer the reason carries. A merge to the default branch produces a run that names none — `workflow_run.pull_requests` is empty — so the one failure that blocks every agent's next pull request is the one delivery nobody hears about. Four ordinary `push` runs failed on `main` in `aivara-se/.website` on 2026-10-09 and 2026-10-10, and by `010`'s rule each of them woke nobody.

The payload answers it without an API call: `workflow_run.event`, `head_branch` and `actor`, and `repository.default_branch` (`octokit/webhooks`, `payload-schemas/api.github.com/common/workflow-run.schema.json` and `common/repository.schema.json`, read 2026-10-21). The actor of a `push` run is the account that pushed, which under a rebase merge is the account that landed the change.

## Decision

- **A second instance of `workflow_run` acts**: action `completed`, conclusion `failure`, `event` `push`, `workflow_run.pull_requests` empty, and `head_branch` equal to `repository.default_branch`. The default branch is read from the payload, not configured.
- **The wake target is `workflow_run.actor`**, matched against the configured agents like any other login.
- **A cap of five wakes per repository and branch**, the shape and lifetime of `010`'s counter and its own key.
- **The reason names the ref rather than a number**: `Dispatch: checks failed on <repository>@<branch>`.
- **No API, no credential, no durable state, no new subscription.**

## What this overrides

`010`'s "at least one entry in `workflow_run.pull_requests`" as the event's only instance, and its "cap of five wakes per pull request" as the counter's only key. The actor exception it created is unchanged in rule and wider in reach: the pusher of a failed run is the account the failure is news to, whether the run names a pull request or the default branch. When the code lands it also retires PRODUCT's edge bullet that a failing run naming no pull request wakes nobody, and its acceptance line that says so. Everything else in `001`–`013` stands.

## Consequences

- A red `main` reaches the account that landed it within a minute, while it still has the context to revert.
- The account woken is the merger, not the author: a rebase merge makes the lander the pusher, and the author is not resolved because that would take an API call.
- A failing run on the default branch that is not a `push` — a schedule, a dispatch, another workflow — wakes nobody, so a nightly that goes red stays the operator's to see.
- A break on `main` that five failed merges have not fixed stops reaching the agent and becomes the operator's.
- The count dies with the process, as every count does.
