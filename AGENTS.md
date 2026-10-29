# Agent Instructions

Dispatch is a Go service that receives GitHub's webhook deliveries for the `aivara-se` organisation and wakes the agent the event concerns — the reviewer a pull request asks for, the author whose review requests changes, or the agent an issue is assigned to — with the reason. It is built for the operator of a small fleet of agents.

## Current Project Focus

v1.2 is built. v1.3 — an issue's assignment wakes its agent — is designed and in review ([#119](https://github.com/aivara-se/dispatch/pull/119)); the receiver branch and its tests follow in one pull request, and the `issues` subscription on the organisation webhook is the operator's. v1.3 adds no state, no count and no GitHub call, and still touches no board.

This section is steering, not policy. It is the one place where what matters right now outranks the standing rules below, it changes often, and it is replaced rather than appended to. Keep it short enough to read in full, and current enough to be worth reading.

## House rules

- **Never** parse a request body before its signature verifies; **never** log a payload body or a secret value, at any verbosity — not the value, not a prefix, not a hash of it.
- **Never** call GitHub's API. The delivery's payload and the configuration answer the whole decision, and the service holds no GitHub credential ([`002`](docs/adrs/002-github-webhook-ingestion.md)).
- **Never** start an agent the event does not call for, and **never** the operator; **never** hold a wake back; and **never** carry text a third party wrote into a run — the reason is identifiers and the service's own words ([`002`](docs/adrs/002-github-webhook-ingestion.md)).
- **Never** keep durable state of the service's own: no database, no service-owned queue, no ordering of events. The one durable thing the service writes is the audit record ([`005`](docs/adrs/005-the-audit-record.md)).
- **Always** keep secret values out of this repository: the configuration names a secret and holds none, and a name that resolves to nothing stops the process at boot rather than at the first delivery.
- **Always** record a significant technology choice as an ADR before the code that depends on it lands.
- The configuration is **JSON**, standard library only: `encoding/json`, no second format anywhere in the service, and no dependency until a real need is written down in the pull request that adds it.

## Tooling

- **Bun is the runtime for scripts.** A script that runs commands — a check, a build, a release, a data fix — is written in TypeScript and run with `bun`: `bun run scripts/<name>.ts`. **Never** Python; prefer it over a bash shell script, because a shell script past a handful of lines has no types, no argument handling and no error handling. A one-line command typed at the prompt is not a script.
- **Never** add a second package manager, a second lockfile, a second formatter or a second test runner. The toolchain is the one the repository already uses, declared in the files it already has.
- **Never** report "tests pass", "it builds" or "verified" without the command and the tree it ran against.

## Verify before pushing

```bash
gofmt -l .        # empty output is the pass
go vet ./...
go test ./...
```

Run the whole sequence, not just its fast part, and read every result — the exit code of the last command says nothing about the first.

These three are the gate, and CI runs them on every pull request and on `main`. What no machine can see, a reviewer covers: that every relative link in a document resolves to a file in the tree, that no unfilled slot survives, and that each ADR's Consequences says what its choice costs and not only what it buys.

## Version Control

- **Branches**: lowercase, hyphens only, one per task, named for the change — `fix-log-timezone`, `chore/adopt-agents-config`. No uppercase, no underscores, no personal prefixes.
- **Commits**: Conventional Commits, lowercase, single line, no scopes — `type: short description`.
- **Never** commit to `main` directly. **Never** force-push a branch another agent or person has seen.
- Keep history linear: no merge commits, no empty commits, no work-in-progress commits left behind.
- Commit under your own identity — your name, your address at this organisation. Never a generic agent, never another agent's identity.
- Remote work is always a branch plus a pull request. The pull request body says what changed, what was verified and how, and what was left out; request review from the operator (`thani-sh`) and one peer agent. Leave the working tree clean: no scratch files, no editor backups, no `.env` you created.

## Repository Structure

- `cmd/dispatch/`: the wire-up — the one flag, the server and its timeouts, and the clean shutdown
- `internal/config/`: loads and validates the configuration file, and resolves the secret it names
- `internal/receiver/`: verifies a delivery, decides, and answers; the dedup cache lives here
- `internal/run/`: starts `hermes --profile <profile> chat -q "<reason>"` and reaps it
- `internal/audit/`: the JSON Lines record, appended and size-rotated
- `docs/PRODUCT.md`: what the service is for, who it is for, and how it is judged
- `docs/SYSTEMS.md`: the architecture — where it runs, how a delivery becomes a run, and what is checked
- `docs/adrs/`: one decision per file; merged into `main` means Accepted
- `AGENTS.md`: the entry file an agent reads first
- `README.md`: how it builds, runs and deploys, and where the documents are

New markdown goes in the directory that already owns its subject, and a fact has exactly one home. Never add a second copy of something a document already says; link to it. If a path in the map above stops being true, fix the map in the same pull request. A map that lies is worse than no map.
