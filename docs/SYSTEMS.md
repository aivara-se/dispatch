# dispatcher: systems

## Where it runs

- One long-running process on the operator's host, beside the Hermes home, as a systemd user unit that restarts on failure. GitHub is its only inbound client.
- Written in **Go** and built as one static binary, `cmd/dispatcher`, with no runtime to install on the host.
- It binds loopback only: `127.0.0.1:8645`, serving `POST /github`. **Caddy** is the reverse proxy in front of it and the only thing on the host that faces the network: it terminates TLS, obtaining and renewing the certificate itself. The port and the path are provisional until the deployment lands.
- The parts: a **receiver** that verifies and deduplicates a delivery, a **board** that reads and writes GitHub, a **router** that turns a delivery into decisions, a **runner** that starts and tracks an agent's run, a **config** loader, and an **audit** writer. `cmd/dispatcher` is the binary that boots them.

## The request path

1. GitHub posts to the public URL, and Caddy forwards it to the loopback port.
2. The receiver reads the raw body up to the body limit, then verifies `X-Hub-Signature-256` against the route's secret. Nothing is parsed before this passes.
3. It takes the event and the delivery id from the headers, and the repository and the action from the parsed body.
4. The repository and the event must be on the allowlist. Anything else is answered `202` and dropped.
5. The delivery id is checked against the cache. A delivery already seen is answered `202` and starts nothing.
6. The router works out which agents the fact concerns. It reads the board for what the delivery cannot state — a card's column and its assignee — and the run table for whether a run is in flight.
7. Where the rule assigns a card, the write happens before the run: the assignee is written, and a write that fails fails the delivery.
8. The runner starts the target agent's run with the reason, and records the run — its profile, what it concerns, and the session id it started.
9. One audit line is appended for every look, whatever happened: the trigger — a delivery that passed verification, a run ending, or boot — the delivery ids where there were any, the decision, the card the run concerns where it concerns one, the assignee it wrote, the run and its session id, and the answer the sender was given where there was one.

The inbound headers read are `X-Hub-Signature-256`, `X-GitHub-Event` and `X-GitHub-Delivery`. The answer set is the service's own (`ADR 006`): `202` accepted — acted on, or seen and deliberately not; `400` a body that is not the JSON the signature covered; `401` a missing or non-verifying signature; `404` a path served by nobody; `413` the body limit; `429` the inbound rate limit, whose limit, window and order against the audit append are `ADR 006`'s; `502` the call or the run the delivery needed failed.

## The state pass

- The board is read in full — every card and its column — at the three moments [`008`](adrs/008-the-service-acts-on-the-board.md) fixes, and never on a payload's say-so alone.
- Each card is then asked the question `PRODUCT.md` (*The rule*) answers: what moves it forward, who does it, and is it already happening. The list there is the whole rule; this section is only where the state comes from.
- **Is it already happening** is the service's own run table, held in memory and rebuilt at boot from the cards in `Started` and the processes it finds. The board cannot answer it: a review wake names an agent whose card never moves, and most runs a delivery starts belong to no card at all.
- The pass writes the assignee and nothing else, and it does not repeat a wake. An action it has attempted for a card is attempted again when the card has changed since, or when that attempt never started a run — a wake that started nothing repairs nothing. **Unsettled** means a card whose next action the pass has not attempted since the card last changed, or whose attempt never started a run.
- **One run per agent per look**, which is the runner's rule: a look that finds twenty unsettled cards for one agent starts one run, concerning the lowest-numbered of them. Where a delivery names its own target, the run concerns what the delivery names — a card or a pull request — and a run may concern no card at all, which is most of them.
- **Looks run one at a time.** The receiver verifies, deduplicates and answers; the pass itself is serial, so two deliveries never decide against the same run table concurrently.

## The board

- The board is one GitHub project in the organisation; a card is an issue, and its column is the project's `Status` field.
- Reading the board is GitHub's GraphQL API — the project-items query, one page at a time — and a card's column comes from it; reading one card for its assignee is the REST issue endpoint. Both use the board token, built once at boot; `ADR 005` names the surfaces and the sources behind them.
- The write is one field: the assignee of the card the rule names. It is the issue's assignee, which is the field the project shows. The service moves no card, writes no comment and closes nothing.
- A read or a write that fails is an error, never a silent "nobody to wake": the delivery is refused, and the wake waits for the next look, not the sender's (*Failure modes*).

## Starting a run

- **The requirement**: start one bounded run of the named agent's own profile, hand it the reason, and know while it is running.
- **The mechanism**: the runner launches that profile's agent as a child process of the service — the Hermes CLI on the profile, `hermes --profile <bot> chat -q "<reason>"` — and tracks the child: running, exited, and its exit code. The run is the profile's own: its prompt, skills, memory and delivery come from the profile, and the dispatcher adds only the reason.
- **One run per agent**: the runner starts no second run for a profile that already has one, and the run table is what it reconciles against after a restart.
- **The reason** is the run's input and nothing else: identifiers, and a sentence the service wrote. Text a third party wrote never reaches it (`ADR 009`), because the reason is an argument to a program that holds tools and credentials.

## GitHub access

- **One organisation webhook**, not one per repository, covers a new repository with no second setup step; an organisation webhook is organisation-owner administration, so the operator creates it (observed on the live organisation, 2026-10-12). It subscribes to every event the service acts on, and the allowlist makes that safe.
- **One token** reads the project and the issues and writes the assignee, with the scopes `ADR 005` fixes. Read-mostly, one field written.
- A delivery is one trigger; the API answers what a payload does not carry. What the payload is for is `ADR 005`'s.

## Configuration and secrets

- The config file is committed to this repository and holds no secret: the listen address, the allowlist of repositories and events, the project, each agent's profile and GitHub login, and the *name* of each secret. It is committed because it is behaviour, and its diff is reviewed like any other.
- Secret values live outside it: an environment file the unit loads, or a file with mode `600` owned by the service user. The config names the variable or the path, and a reference that resolves to nothing stops the process at boot rather than at the first event.
- Nothing secret is logged, ever — not the value, not a prefix, not a hash of it. No payload body is logged either: an audit line carries identifiers, and a payload can hold anything a third party wrote.

## Storage

- **No database.** The board is the durable state; the service holds no source of truth of its own.
- In memory: the delivery-id cache, with a one-hour TTL, the run table, and the per-card record of the action last attempted for it — what the pass reads to avoid repeating a wake. None of the three survives a restart.
- On disk: the audit log, one JSON Lines line per look in arrival order, size-rotated; and the dead-letter file for a delivery or a run that failed. The service appends to both and rewrites neither.

## Failure modes

- **A duplicate delivery** is answered from the cache; **an out-of-order one** is unaffected, because nothing orders events and every wake is a reason to re-read.
- **A restart** loses the cache and the run table, not the audit log; the pass at boot re-reads the board.
- **A run that cannot be started**, or **a board read or write that fails**, is retried a bounded number of times and then dead-lettered with its reason, and the delivery is refused. GitHub does not redeliver a failed delivery (*Handling failed webhook deliveries* and *Redelivering webhooks*, docs.github.com, read 2026-10-12): it keeps the delivery in its list for three days, and the dead-letter file holds the same wake. The next look repairs it — the card is still unsettled, so the pass attempts the wake again. A refused wake is late, never lost.
- **A wrong signature or an oversized body** is refused before anything is parsed, and **an event that is not ours** is answered `202` and dropped.
- **GitHub unreachable** — the board read fails and the delivery is refused rather than answered done; the service keeps no queue of its own and invents no ordering.

## Checks

- The gate runs on every pull request and on `main`: `gofmt -l .`, `go vet ./...` and `go test ./...`.
- The layers those tests must cover, and the ones they cannot: the signature and the refusal before parsing, the dedup, the router's decision per payload fixture against a stubbed board, the assignee write, the pass over a fixture board at boot, the runner's start and its tracking. No test covers a real delivery through the live GitHub API or a real agent run; those are checked by hand on the host.
