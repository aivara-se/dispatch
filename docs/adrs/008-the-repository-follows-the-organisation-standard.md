# 008 - The repository follows the organisation's standard

## Context

Dispatch grew its own entry file, kept its behaviour and system documents at the root, and carried a design document in a richer format than the organisation uses. The organisation now keeps one entry file, one document standard and three reusable checks, and a repository whose copies drift fails its pull request. This repository was behind that baseline.

## Decision

- `AGENTS.md` is a byte-identical copy of the organisation's entry file; the copy is never edited.
- `README.md` is the entry point; `docs/SPEC8N.md` owns direction, requirements and architecture; `docs/DESIGN.md` owns the interface tokens; `docs/adrs/` owns the decisions.
- The `validate` workflow runs the organisation's entry-file, document and settings checks as three jobs, and fails the required `validate` job when any of them fails.
- The repository's own checks stay in the `validate` job, including the formatter.
- The engine stays in Go and the tooling stays in Bun and TypeScript. This split is the repository's stated exception to the organisation's TypeScript rule.

## Consequences

- A rule changes once in the organisation's repositories and reaches this one at its next merge.
- The root carries no behaviour or system document; a fact that used to live there lives in `docs/SPEC8N.md`.
- The settings check fails the pull request until the repository's live settings match the baseline, so settings are aligned before a change is opened.
- The Go engine reads and builds as before; only its documentation and its checks moved.
