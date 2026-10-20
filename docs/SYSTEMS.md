# Dispatch: systems

## Where it runs

One long-running process on the operator's host, beside the Hermes home, as a systemd user unit that restarts on failure. It binds loopback and is reached through the host's reverse proxy, which terminates TLS; GitHub is its only inbound client. The binary is static and the deploy is a new binary plus a unit restart — there is no deploy pipeline ([`001`](adrs/001-go-and-the-standard-library.md)).

The unit, the environment file and the proxy site are `README.md` (*Run it on the host*).

## A delivery becomes a run

1. GitHub posts to the public URL; the proxy forwards to the loopback address and path the configuration names ([`004`](adrs/004-configuration.md)).
2. The receiver reads the raw body up to 1 MiB, then verifies `X-Hub-Signature-256` against the configured secret. Nothing is parsed before it verifies ([`002`](adrs/002-github-webhook-ingestion.md)).
3. It takes the event and the delivery id from the headers, and the action, the repository and — per event — the pull request, the requested reviewer, the review state, the author or the assignee, the issue and its assignee, or the run's actor and the pull requests it names, from the body.
4. Five deliveries act: a `pull_request` delivery whose action is `review_requested`, which wakes the requested reviewer; a `pull_request_review` delivery whose action is `submitted` and whose review state is `changes_requested`, which wakes the author ([`006`](adrs/006-the-review-loop.md)); an `issues` delivery whose action is `assigned`, which wakes its assignee ([`008`](adrs/008-issue-assignment.md)); a `pull_request` delivery whose action is `assigned`, which wakes its assignee ([`009`](adrs/009-every-assignment-wakes-its-agent.md)); and a `workflow_run` delivery whose action is `completed`, whose conclusion is `failure` and which names at least one pull request, which wakes the run's actor ([`010`](adrs/010-a-failing-check-wakes-the-pusher.md)). Every other event, action or review state is accepted and ignored.
5. The delivery id is checked against the in-memory cache. A delivery already seen is accepted and starts nothing.
6. For the review flows, the round count decides whether the delivery may act: at ten it wakes nobody, and below ten a `changes_requested` review adds one ([`007`](adrs/007-the-review-cap.md)). A failed check keeps a count of its own, five per pull request, checked before the wake and advanced only when a run starts ([`010`](adrs/010-a-failing-check-wakes-the-pusher.md)). Both counts are per pull request and in memory. An assignment keeps no count and consults neither.
7. The login to wake is matched against the configured agents — the requested reviewer, the pull request's author, the delivery's assignee, or the run's actor. A login the configuration does not carry wakes nobody, and the delivery's own actor is never woken, except the actor of a failed run, who is the one it is news to.
8. The matched profile's agent is started, with the reason ([`003`](adrs/003-starting-a-run.md)).
9. One audit line is appended whatever happened ([`005`](adrs/005-the-audit-record.md)).

The answers are the service's own: `401` a missing or non-verifying signature, `400` a body it will not read, `202` every delivery that verifies — acted on or ignored. The audit line, not the status, tells those two apart.

## Starting a run

The requirement is that a review request, an assignment or a failing check is never delayed. The mechanism is a child process: `hermes --profile <profile> chat -q "<reason>"`, launched by the service and not tracked ([`003`](adrs/003-starting-a-run.md)).

- **No serialisation.** Every wake starts a run; nothing queues and nothing is held back, so an agent may have several runs at once.
- **No tracking.** The child is reaped so it does not linger; no run table, no exit bookkeeping, no lifetime bound, nothing reconciled at boot.
- **The binary is absolute**, because a user unit's `PATH` does not resolve the bare command ([`004`](adrs/004-configuration.md)).
- **When it cannot start**, the audit line records it and the delivery is still answered `202`. The service keeps no queue, so that wake is lost rather than retried.

The argv is a contract with another program — `--profile`/`-p` and `chat -q` are Hermes' own. A test can pin the argv the service builds; only a hand check on the host can show that Hermes still accepts it.

## The reason

A wake's only input is the reason, and the reason is identifiers and the service's own words — never a title, a body, a branch or a comment ([`002`](adrs/002-github-webhook-ingestion.md)). The five fixed sentences are:

```
Dispatch: review requested on aivara-se/dispatch#105
Dispatch: changes requested on aivara-se/dispatch#105
Dispatch: issue assigned on aivara-se/dispatch#124
Dispatch: pull request assigned on aivara-se/dispatch#105
Dispatch: checks failed on aivara-se/dispatch#105
```

The service's name tells the agent this is a wake and not the operator speaking; the repository and number are the pointer the agent re-reads; the phrase is the why-now. GitHub sends `review_requested` for both a reviewer added and a review requested again, so that sentence does not claim to tell them apart.

## GitHub access

One organisation webhook, created by an owner of the organisation, subscribed to `pull_request`, `pull_request_review`, `issues` and `workflow_run`, and set to content type `json` — a `form` webhook sends `payload=<urlencoded>`, which the JSON decode refuses. Its secret is the value the configuration names, held in the unit's environment and never in the tree. The `issues` subscription is what an issue's assignment needs, `pull_request` carries a pull request's assignment and its reviews, and `workflow_run` carries a failing check; a webhook missing one of them receives none of that flow.

The service calls no GitHub API and holds no GitHub credential: the payload and the configuration answer the whole decision ([`002`](adrs/002-github-webhook-ingestion.md)). The agents act on GitHub with their own credentials, not the service's.

## Storage

- **In memory, and only there:** the delivery-id cache for the TTL `002` fixes, each pull request's round count ([`007`](adrs/007-the-review-cap.md)) and its failed-check count ([`010`](adrs/010-a-failing-check-wakes-the-pusher.md)), and the configuration and secret resolved at boot.
- **On disk:** the audit file alone — one JSON Lines line per delivery, appended, size-rotated ([`005`](adrs/005-the-audit-record.md)).
- **Nothing else.** No database, no queue, no ordering of events, and no state reconciled after a restart.

## Failure modes

- **A wrong or missing signature** — `401`; nothing is parsed and nothing is logged.
- **A body that is not JSON, or past the 1 MiB bound** — refused, answered `400`.
- **A delivery that is not ours** — another event, action or review state: `202`, and nobody is woken.
- **A repeat** — `202`, and nobody is woken; the line shows it was seen.
- **A pull request at either cap** — `202`, and nobody is woken; the line shows which cap was reached.
- **A run that cannot start** — recorded on the line, answered `202`, not retried.
- **A crash or a restart** — out of scope: the cache, the round counts and any run in flight are not reconciled, and a delivery redelivered inside the lost hour can start one extra run.

## Checks

```sh
gofmt -l .        # empty output is the pass
go vet ./...
go test ./...
```

The tests must cover: the signature refused before parsing, the event and review-state filter, the assignment filter on issues and on pull requests, the failed-check filter and its cap, the dedup, the ten-round cap and the wake it refuses, the configuration loader's refusals, and one signed POST per flow that starts a run. The reason tests use a fixture whose every text field is a sentinel — no sentinel reaches the argv — and the argv is a fixed sentence above. A fake `hermes` on the configured path captures the argv.

What no test can reach: that Hermes still accepts the argv, and a real delivery through GitHub to a real agent run. Both are checked by hand on the host.

## Planned, not built

The versions [PRODUCT](PRODUCT.md) (*Roadmap*) decides, and the records that fix them. Nothing in this section is in the tree: the sections above describe v1.5, and each of these lands with the tests that name it.

- **v1.6 — a mention** ([`011`](adrs/011-a-mention-wakes-the-agent-it-names.md)). `issue_comment` with action `created` wakes each configured login named exactly in the body, and only when `comment.author_association` is `OWNER`, `MEMBER` or `COLLABORATOR`. The reason gains `Dispatch: mentioned on <repository>#<number>`, and the webhook gains `issue_comment`.
- **v1.7 — an approval that lands nothing** ([`012`](adrs/012-an-approval-that-lands-nothing-wakes-the-author.md)). `pull_request_review` with action `submitted`, review state `approved`, on a pull request that is open, not a draft and carries `auto_merge` null, wakes the author. The reason gains `Dispatch: approved but not armed on <repository>#<number>`. No new subscription and no new state.
- **v1.8 — a draft marked ready** ([`013`](adrs/013-a-draft-marked-ready-wakes-its-reviewers.md)). `pull_request` with action `ready_for_review` wakes every configured login in `requested_reviewers`; a team is not resolved. The reason gains `Dispatch: ready for review on <repository>#<number>`. No new subscription and no new state.

The answer set, the audit line's fields and the on-disk record are unchanged by all three: `202`, the same line, the same file, and no counter beyond the two Storage lists. The tests each version adds are the filters it introduces — the mention match and the association guard, the unarmed approval, the ready-for-review reviewers — and the one no test can reach stays the hand check on the host.
