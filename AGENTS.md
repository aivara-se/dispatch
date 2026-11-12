# Agent Instructions

This file is identical in every `aivara-se` repository. Edit `aivara-se/.github`; a differing copy fails.

## Read first

- `README.md` — what this is, how to run it, and how to check it.
- `PRODUCT.md` (root or `docs/`) — who it serves, and what would make it done.
- `SYSTEMS.md`, where present — how it is shaped and what it depends on.
- `DESIGN.md` — the interface values, where present.
- `docs/adrs/` — a decision per file, written before the code it decides.
- `SETTINGS.md`, `aivara-se/.github/docs` — every repository's baseline.

Link to a document; never restate it. A per-repository rule belongs in `SYSTEMS.md`.

## House rules

- Never commit a secret; a configuration file names one, holding no value.
- Never report "tests pass" without the command and the tree.
- Never add a second package manager, lockfile, formatter or test runner; use the one it carries.
- A script that runs commands is TypeScript on `bun`; shell only for a few lines.
- Open the skill whose `when-to-use` covers the work, in `aivara-se/.agents/skills`; no copy exists.
- Never edit a merged ADR. Supersede it, naming the clauses it overrides.

## Version control

- Branch: lowercase and hyphens, one per task — `docs/`, `feat/`, `fix/`, `chore/` and a short name.
- Commit: Conventional Commits, lowercase, single line, no scope.
- Never commit to `main`; never force-push a branch another person has seen; keep history linear.
- Commit under your own name.
- Land a branch and a pull request. The body says what changed, what was verified, what was left out. Leave the tree clean.
- The operator reviews a documents-only change alone; an implementation adds a peer.
