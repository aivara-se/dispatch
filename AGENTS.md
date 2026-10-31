# Agent Instructions

Dispatch is a Go service that receives GitHub's webhook deliveries for the `aivara-se` organisation and wakes the agent the event concerns — the reviewer a pull request asks for, the author whose review requests changes, or the agent an issue is assigned to — with the reason. It is built for the operator of a small fleet of agents.

This file is the `aivara-se` agent convention: the rules that hold in every repository. Adopt it, do not fork it. It never describes the state of the work — that lives in the documents below.

## Where a fact lives

- `docs/PRODUCT.md` — what the service is, who it serves, what it must not do, what the work is on now, and what would make it done.
- `docs/SYSTEMS.md` — how it is served, built and checked.
- `docs/adrs/` — a decision per file, written before the code it decides.
- `README.md` — how to run it locally.

One fact, one home: link to a document, never restate it. A sentence here that starts describing the work instead of how work is done belongs in `PRODUCT.md`.

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

## Skills

The organisation's skills live in `aivara-se/.agents/skills` and every agent reads them from there; a repository carries no copy. Each declares a `when-to-use`: open the one that covers the work before starting it.

## Version Control

- **Branches**: lowercase, hyphens, one per task: `docs/`, `feat/`, `fix/` or `chore/` and a short name — `docs/adopt-agents-config`, `fix-log-timezone`. No uppercase, no underscores, no personal prefixes.
- **Commits**: Conventional Commits, lowercase, single line, no scopes — `type: short description`.
- **Never** commit to `main` directly. **Never** force-push a branch another agent or person has seen.
- Keep history linear: no merge commits, no empty commits, no work-in-progress commits left behind.
- Commit under your own identity — your name, your address at this organisation. Never a generic agent, never another agent's identity.
- Remote work is always a branch plus a pull request. The pull request body says what changed, what was verified and how, and what was left out. Leave the working tree clean: no scratch files, no editor backups, no `.env` you created.
- Request review from the operator (`thani-sh`) alone on a documents-only change, and from one peer agent as well on an implementation one.
