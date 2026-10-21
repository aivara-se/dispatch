# Dispatch: systems

This document describes v1.14; the flows and their acceptance are [PRODUCT](PRODUCT.md).

## Where it runs

One long-running process on the operator's host, beside the Hermes home, as a systemd user unit that restarts on failure. It binds loopback and is reached through the host's reverse proxy, which terminates TLS; GitHub is its only public client, and a collector on the same host reads its metrics over loopback ([`020`](adrs/020-telemetry-leaves-through-a-collector.md)). The binary is static and the deploy is a new binary plus a unit restart — there is no deploy pipeline ([`001`](adrs/001-go-and-the-standard-library.md)).

The unit, the environment file and the proxy site are `README.md` (*Run it on the host*).

## A delivery becomes a run

1. GitHub posts to the public URL; the proxy forwards to the loopback address and path the configuration names ([`004`](adrs/004-configuration.md)).
2. The receiver reads the raw body up to 1 MiB, then verifies `X-Hub-Signature-256` against the configured secret. Nothing is parsed before it verifies ([`002`](adrs/002-github-webhook-ingestion.md)).
3. It takes the event and the delivery id from the headers, and from the body the action, the repository and its default branch, and — per event — the pull request with its state, draft flag, `auto_merge` and `merged_at`, the requested reviewer or the reviewers still requested, the review's state, body and author association, the pull request's author, the assignee, the issue, the run's actor, event, head branch and the pull requests it names, or the comment's body and author association.
4. These deliveries act:
   - a `pull_request` delivery whose action is `review_requested`, which wakes the requested reviewer;
   - a `pull_request_review` delivery whose action is `submitted`, which wakes the pull request's author when the review state is `changes_requested` ([`006`](adrs/006-the-review-loop.md)) and, when the pull request is open, not a draft and carries `auto_merge` null, also when the state is `approved` ([`012`](adrs/012-an-approval-that-lands-nothing-wakes-the-author.md));
   - an `issues` delivery whose action is `assigned`, which wakes its assignee ([`008`](adrs/008-issue-assignment.md));
   - a `pull_request` delivery whose action is `assigned`, which wakes its assignee ([`009`](adrs/009-every-assignment-wakes-its-agent.md));
   - a `workflow_run` delivery whose action is `completed`, whose conclusion is `failure` and which names at least one pull request, which wakes the run's actor ([`010`](adrs/010-a-failing-check-wakes-the-pusher.md));
   - a `workflow_run` delivery whose action is `completed`, whose conclusion is `failure`, whose event is `push`, which names no pull request and whose `head_branch` is the repository's default branch, which wakes the run's actor ([`014`](adrs/014-a-red-main-wakes-its-pusher.md));
   - an `issue_comment` delivery whose action is `created` and whose comment's author is the owner, a member or a collaborator, which wakes each configured login the body names ([`011`](adrs/011-a-mention-wakes-the-agent-it-names.md));
   - a `pull_request_review` delivery whose action is `submitted`, whose review body is read under the same association guard and wakes each configured login it names ([`016`](adrs/016-a-mention-reads-the-reviews-own-surfaces.md));
   - a `pull_request_review_comment` delivery whose action is `created`, under the same guard and the same rule;
   - a `pull_request` delivery whose action is `ready_for_review`, which wakes every configured login in `requested_reviewers` ([`013`](adrs/013-a-draft-marked-ready-wakes-its-reviewers.md));
   - a `pull_request_review` delivery whose action is `dismissed`, which wakes the pull request's author when the pull request is open, not a draft and carries no `merged_at` ([`017`](adrs/017-a-dismissed-review-wakes-the-author.md)).

   Every other event, action, review state or author association is accepted and ignored.
5. The delivery id is checked against the in-memory cache. A delivery already seen is accepted and starts nothing.
6. For the review flows, the round count decides whether the delivery may act: at ten it wakes nobody, and below ten a `changes_requested` review adds one ([`007`](adrs/007-the-review-cap.md)). A delivery with a mention is not refused by that count: it wakes the logins its text names and loses only the review's own target. A failed check keeps a count of five per pull request ([`010`](adrs/010-a-failing-check-wakes-the-pusher.md)), and one of five per repository and branch ([`014`](adrs/014-a-red-main-wakes-its-pusher.md)); each is checked before the wake and advanced only when a run starts. Every count is in memory. An assignment, an approval, a mention, a dismissal and a ready-for-review keep no count and consult none.
7. The logins to wake are matched against the configured agents: the requested reviewer, the pull request's author, the delivery's assignee, the run's actor, the logins a comment or a review names, or the reviewers a pull request still requests. A team entry is never resolved, so it wakes nobody. A login the configuration does not carry wakes nobody, and the delivery's own actor is never woken, except the actor of a failed run, who is the one it is news to.
8. Each matched profile's agent is started, with the reason ([`003`](adrs/003-starting-a-run.md)). One delivery that wakes several agents starts one run per profile and records them on one line, comma-separated.
9. An audit line is appended whatever happened ([`005`](adrs/005-the-audit-record.md)). A run that ends non-zero appends a line of its own ([`015`](adrs/015-the-audit-records-a-runs-exit.md)), and each boot appends one ([`018`](adrs/018-the-services-boot-reaches-the-audit.md)).

The answers are the service's own: `401` a missing or non-verifying signature, `400` a body it will not read, `202` every delivery that verifies — acted on or ignored. The audit line, not the status, tells those two apart.

## Starting a run

The requirement is that no wake is delayed. The mechanism is a child process: `hermes --profile <profile> chat -q "<reason>" --source dispatch`, launched by the service ([`003`](adrs/003-starting-a-run.md), [`019`](adrs/019-every-run-is-tagged-dispatch.md)).

- **No serialisation.** Every wake starts a run; nothing queues and nothing is held back, so an agent may have several runs at once.
- **No tracking.** The child is waited on from its own goroutine, its exit status is read once, and nothing about the run is stored: no run table, no lifetime bound, nothing reconciled at boot ([`015`](adrs/015-the-audit-records-a-runs-exit.md)). A run that exits non-zero appends a line of its own; one that exits zero appends nothing.
- **The binary is absolute**, because a user unit's `PATH` does not resolve the bare command ([`004`](adrs/004-configuration.md)).
- **When it cannot start**, the audit line records it and the delivery is still answered `202`. The service keeps no queue, so that wake is lost rather than retried.

The argv is a contract with another program — `--profile`/`-p`, `chat -q` and `--source` are Hermes' own. A test can pin the argv the service builds; only a hand check on the host can show that Hermes still accepts it.

## The reason

A wake's only input is the reason, and the reason is identifiers and the service's own words — never a title, a body or a comment ([`002`](adrs/002-github-webhook-ingestion.md)). The one branch is the red-main reason's ref, which stands where a pull request's number would ([`014`](adrs/014-a-red-main-wakes-its-pusher.md)). The ten fixed sentences are:

```
Dispatch: review requested on aivara-se/dispatch#105
Dispatch: changes requested on aivara-se/dispatch#105
Dispatch: approved but not armed on aivara-se/dispatch#105
Dispatch: issue assigned on aivara-se/dispatch#124
Dispatch: pull request assigned on aivara-se/dispatch#105
Dispatch: checks failed on aivara-se/dispatch#105
Dispatch: checks failed on aivara-se/dispatch@main
Dispatch: mentioned on aivara-se/dispatch#124
Dispatch: ready for review on aivara-se/dispatch#105
Dispatch: approval dismissed on aivara-se/dispatch#105
```

The submitted review carries two, because a review that requests changes and an approval that lands nothing wake the same author for different reasons; the mention is read from a comment, a review body or a review comment.

The service's name tells the agent this is a wake and not the operator speaking; the repository and number are the pointer the agent re-reads; the phrase is the why-now. GitHub sends `review_requested` for both a reviewer added and a review requested again, so that sentence does not claim to tell them apart.

## GitHub access

One organisation webhook, created by an owner of the organisation, subscribed to `pull_request`, `pull_request_review`, `pull_request_review_comment`, `issues`, `issue_comment` and `workflow_run`, and set to content type `json` — a `form` webhook sends `payload=<urlencoded>`, which the JSON decode refuses. Its secret is the value the configuration names, held in the unit's environment and never in the tree. `issues` is what an issue's assignment needs, and `issue_comment` a mention on an issue or on a pull request; `pull_request` carries a pull request's assignment, its review requests and a draft marked ready; `pull_request_review` a submitted or a dismissed review and the mention in a review's body; `pull_request_review_comment` the mention in an inline comment; and `workflow_run` a failing check. A webhook missing one subscription receives none of that flow.

The service calls no GitHub API and holds no GitHub credential: the payload and the configuration answer the whole decision ([`002`](adrs/002-github-webhook-ingestion.md)). The agents act on GitHub with their own credentials, not the service's.

## Storage

- **In memory, and only there:** the delivery-id cache for the TTL `002` fixes, each pull request's round count ([`007`](adrs/007-the-review-cap.md)), its failed-check count ([`010`](adrs/010-a-failing-check-wakes-the-pusher.md)) and each repository and branch's failed-check count ([`014`](adrs/014-a-red-main-wakes-its-pusher.md)), and the configuration and secret resolved at boot.
- **On disk:** the audit file alone — one JSON Lines line per delivery, one per run that ended non-zero, and one per boot, appended and size-rotated ([`005`](adrs/005-the-audit-record.md), [`015`](adrs/015-the-audit-records-a-runs-exit.md), [`018`](adrs/018-the-services-boot-reaches-the-audit.md)).
- **Nothing else.** No database, no queue, no ordering of events, and no state reconciled after a restart.

## Failure modes

- **A wrong or missing signature** — `401`; nothing is parsed and nothing is logged.
- **A body that is not JSON, or past the 1 MiB bound** — refused, answered `400`.
- **A delivery that is not ours** — another event, action, review state or author association: `202`, and nobody is woken.
- **A delivery with nobody to wake** — a team in place of a login, a comment, review or review comment whose author is not the owner, a member or a collaborator, an unconfigured login: `202`, and the line names which of them it was. An approval on a draft or an armed pull request, and a dismissal on a draft, a closed or a merged one, is refused the same way, but its line names no condition: it is logged as an event and an action that do not act.
- **A repeat** — `202`, and nobody is woken; the line shows it was seen.
- **A pull request at either cap, or a branch at its cap** — `202`, and nobody is woken; the line shows which cap was reached.
- **A run that cannot start** — recorded on the line, answered `202`, not retried.
- **A run that ends non-zero** — a line of its own, appended when the child exits; the delivery's answer went out before it started, so nothing changes and nothing is retried.
- **A crash or a restart** — the boot line marks it; the cache, the round counts and any run in flight are not reconciled, and a delivery redelivered inside the lost hour can start one extra run.

## Planned, not built

Every list above describes v1.14; two things are decided and unbuilt.

**One run per pull request per agent** ([`021`](adrs/021-one-run-per-pull-request-per-agent.md)). A gate keyed `<repository>#<number>` and profile — `<repository>@<branch>` and profile for a default-branch run — holds at most one delivery per key. A delivery that lands while that key's run is live starts nothing; the run in flight's exit starts one follow-up carrying the most recent held delivery's reason, and the key is released on that exit. The audit gains the decision `held` and a line for the follow-up's start, paired with the delivery's own line by delivery id. The *Starting a run* section above still describes v1.14, which serialises nothing.

**Observability, not a version** ([`020`](adrs/020-telemetry-leaves-through-a-collector.md)). The service serves `GET /metrics` in Prometheus text on `127.0.0.1:9464` — `dispatch_deliveries_total{event,decision}` and a `dispatch_delivery_duration_seconds` histogram — from `internal/metrics`, standard library only. A stock OpenTelemetry Collector on the host scrapes it and exports OTLP to whichever provider the operator configures; nothing in this repository builds, configures or restarts the collector. It adds no dependency, no configuration field, no audit line and no span.

## Checks

```sh
gofmt -l .        # empty output is the pass
go vet ./...
go test ./...
```

The tests must cover: the signature refused before parsing, the event and review-state filter, the assignment filter on issues and on pull requests, the failed-check filter on a pull request and on the default branch with its own cap, the dedup, the ten-round cap and the wake it refuses, the mention match on all three surfaces and its association guard, the unarmed approval and the states that refuse it, the dismissal and the states that refuse it, the ready-for-review reviewer set, the configuration loader's refusals, the run line for a non-zero exit and the absence of one for a zero exit, the boot line, and one signed POST per flow that starts a run. The reason tests use a fixture whose every text field is a sentinel — no sentinel reaches the argv — and the argv is a fixed sentence above. A fake `hermes` on the configured path captures the argv.

What no test can reach: that Hermes still accepts the argv, and a real delivery through GitHub to a real agent run. Both are checked by hand on the host.
