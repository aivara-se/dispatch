# 038 A delivery wakes the agent its login names

## Context

The review flow starts on `pr.created` and names its reviewer in the action: `"agent": "momo"`. The flow author decides who reviews. The delivery already names the logins — `requested_reviewer.login`, `pull_request.requested_reviewers[]`, `assignee.login` — and `agents` maps a name to a profile and nothing to a login, so a delivery resolves to no agent. Two reviewers on one pull request then meet `UNIQUE (flow, owner, repo, number)` in the record: one job starts and the second agent never wakes.

## Decision

- `agents.<name>.login` is required. A login maps to at most one agent, and the load fails on an empty or duplicate login.
- Two conditions join the closed set, proved by a delivery alone: `pr.review_requested` from `review_requested` (its `requested_reviewer`) and from `opened` (each login in `requested_reviewers`); `pr.assigned` from `pull_request.assigned` (its `assignee`). Neither holds after a task.
- A start that names logins starts one job per agent its logins name. A login no agent holds starts no job and records nothing.
- `pr.created`, `pr.approved` and `pr.rejected` name no login, so a job for one takes no agent: its `agent` is the empty string, never null, and the constraint below is total over both kinds of start.
- A job's identity is `(flow, owner, repo, number, agent)`. `{agent}` in a flow's `key` and in an action's `agent` resolves to the job's agent.
- A flow whose start names a login carries `{agent}` in its `key`, and the loader refuses one that does not. Its two agents' jobs for one pull request then take two addresses, `flow/owner/repo/key`, and two sessions.
- The record carries a schema version, and every migration raises it. A process whose own version is older than the file's stops at boot and names the file and both numbers, rather than opening a record it cannot read.
- A task that must run once per pull request stays in a flow whose start is a pull-request condition. Two agents' jobs read `pr.approved` and `pr.rejected` on the pull request, so a merge task inside a per-agent flow runs once per reviewer.

## What this overrides

`029`'s "A `next` key is one of five conditions", `033`'s list of what a flow's `_start_.next` decides and `035`'s "`_start_.next` may name only the conditions a delivery proves" each gain the two conditions, and the rest of each stands: `035` still refuses `pr.updated` and `pr.merged` there. `033`'s "unless that flow already has a job there" and "starts no second job" keep their meaning with the agent inside a job's identity. `030`'s "A job is identified by flow, owner, repo and key" and `034`'s `dispatch show <flow>/<owner>/<repo>/<key>` keep theirs, with the agent inside the key.

## Consequences

- The App's subscription and permissions do not change: both actions arrive on `pull_request`, which it already receives.
- Two reviewers review one pull request at once, in two jobs and two sessions; a client that reads one pull request lists both jobs.
- The record's `jobs` table is rebuilt once, by the first process whose version is newer than the file's: a unique constraint cannot be altered, so the migration copies the table and raises the version, and no later boot repeats it. The first boot after the release runs it against the live record.
- A second merge job cannot appear: `EnsureJob` collapses two approvals of one pull request into the flow's one job there.
- The first rebuild is the only one without a rollback the process enforces: the binaries that predate the version clause carry no check, so a rollback to one of them is the copied database and the binary that wrote it, together. The check is what makes the migration after this one reversible.
- The operator's login, and a login no agent holds, wake nothing and append no line, so a wake that did not happen leaves no trace.
