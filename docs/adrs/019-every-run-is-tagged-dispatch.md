# 019 - Every Run Is Tagged Dispatch

## Context

`003` fixes the argv as `hermes --profile <profile> chat -q "<reason>"`, and the run stores itself with the source tag `oneshot`. That is the tag every finite automation on the host shares, so the service's runs are distinguishable here only because they are the only one-shots on this machine.

Hermes stores a per-session source tag and an explicit `--source` always wins, one-shot runs included; `hermes sessions list --source <tag>` filters on it, and the prune and archive filters take the same key (`hermes-agent`, *Sessions → Session Sources*, read 2026-10-21). The same tag decides whether a session is listed at all: the human pickers exclude three sources — `kanban`, `tool` and `oneshot` — as a deny-list (`hermes_state_sessions.py`, `INTERNAL_LISTING_SOURCES`, read 2026-10-21). A title is not available at spawn: `chat` has no title flag, a title is generated after the first exchange, and setting one by hand takes `sessions rename <id>` and an id the service does not read.

## Decision

- **The argv gains one token**: the run is `hermes --profile <profile> chat -q "<reason>" --source dispatch`.
- **The tag is the literal string `dispatch`**, and nothing in the service reads it back.
- **Nothing else in the service changes**: no title, no rename, no session id, no pid, and nothing reads the tag back. The tag does change where the session is listed, which the consequences below record.

## What this overrides

Nothing falls. `003`'s argv sentence gains a flag, and its "the argv is a contract with another program" now covers one more token. The run stays untracked, the reason is unchanged, no state is added, and the actor, roster and cap rules are untouched.

## Consequences

- **`hermes sessions list --source dispatch` selects the service's runs**, and `oneshot` stops standing in for Dispatch.
- **The run stops being hidden from the human pickers.** `oneshot` is on the deny-list, so today a wake is absent from the TUI, Desktop and dashboard session lists and from `hermes sessions list` (`hermes_state_sessions.py:239`, `hermes_cli/console_engine.py:601`, read 2026-10-21); tagged `dispatch` it is not, and every wake appears in them. The filter is bought with that visibility: a tag inside the deny-list would have gone on hiding. Nothing in the service depends on the pickers, so this is a reading habit, not a behaviour.
- **It does not make a wake addressable.** Every run carries the same tag, duplicates included, so it selects a family and never a session: resuming by title, steering and stopping stay out of reach.
- **A session the operator later continues by hand keeps the tag.** The tag records where a session was started, not who is using it.
- **The flag is a second thing about the argv no test here can verify against Hermes.** The repository's test pins what it sends; a hand check on the host pins that Hermes accepts it.
- **A typo in the tag is silent.** Nothing in the service filters by it, so the operator's habit breaks without a symptom in the audit.
