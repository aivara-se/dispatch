# 022 - A Wake Resumes Its Object's Session

## Context

Every wake starts a new Hermes session: `003` fixes the run as `hermes --profile <profile> chat -q "<reason>"`, and the reason is the run's whole input. The profile's memory, skills and entry file arrive each time, and so does the item the wake names, re-read at its head. What does not arrive is the run's own working state — why the change took this shape, which command failed, what was ruled out. A reviewer asked again on a pull request it reviewed two days ago re-derives its findings from the thread; an author answering a third round re-derives its own design. The ten-round cap (`007`) bounds how often that happens, not what each round costs.

Hermes resumes a chat by name. `-c "<title>"` loads the session carrying it and follows a compression lineage to the newest variant; `--create-if-missing` turns a miss into a new session with that title, in the same process, so resume-or-create is one call and not a check followed by an act (`hermes chat --help`, `hermes_cli/_parser.py`, read 2026-10-27). A title set at creation holds against the auto-titler, which fills an untitled row and upgrades its own `derived` title but not a higher authority (`agent/title_generator.py`, read 2026-10-27). Titles are unique per profile and keep `/` and `#`.

Three facts about the running version bound the choice. Creation goes through the gateway's `session.create`, which stamps the row's source `cli` whatever `--source` the run carries (`hermes_cli/gateway_session_store.py`, read 2026-10-27). `--source` is an enum rather than free text, which is why the run is tagged `tool` and `019`'s value could not stand. And a resumed session keeps its own model and restores its recorded working directory (`hermes-agent`, *Sessions → CLI Session Resume*, read 2026-10-27).

## Decision

- **A wake resumes the session for its object.** The run is `hermes --profile <profile> chat -q "<reason>" --source tool -c "<title>" --create-if-missing`.
- **The title is derived from the delivery, never stored**: `<owner>/<repo>#<number>`, or `<owner>/<repo>@<branch>` for a default-branch run. The profile is the namespace — each profile has its own store — so the agent's name is not in the key.
- **The first wake creates the session and every later one appends to it**, a compression split included, because `-c` resolves to the newest variant of the lineage.
- **The service stores nothing**: no id, no table, no file. The title is recomputed from the delivery; a session that was pruned or renamed is created again and the wake proceeds without it.
- **The wake still re-reads the item at the head it finds.** The session is memory of reasoning, not of state: a tool result from an earlier head is stale, and the item the wake names is authoritative.
- **`021`'s key is the session's lock as well as the review's.** One run in flight per object and profile is what stops two runs appending to one transcript; reuse adds no gate and no record.

## What this overrides

`019`'s consequence "It does not make a wake addressable" falls: a derived title is an address every wake for one object reaches. `019`'s decision stands — the tag selects a family and nothing reads it back — but its value is `tool`, not `dispatch`: Hermes 0.22.5 refuses `dispatch` in `session.create` with `4001 invalid_params`, and `tool` is the label it defines for a third-party integration.

`003`'s "a wake is a reason, not an instruction … the agent re-reads and decides" keeps its force as what the agent does, and loses its guarantee from the mechanism, because a resumed run brings what it read last time. The child process, the absolute path and `003`'s untracked run stand.

`005` is untouched: nothing about a session is the service's to write, and a boot still reconciles nothing.

## Consequences

- **An object accumulates a transcript.** Each wake's input grows with the rounds behind it; past the model's window Hermes compresses and the next wake gets a summary. The value is largest in the early rounds and decays.
- **A session keeps its model.** A changed model or provider reaches a new object, not an open one.
- **A created session is stamped `cli`**, so it appears in the operator's session pickers, which `tool` hides; the run is still tagged `tool`. It gives back part of what tagging the runs `tool` bought.
- **Injection persists.** Text the agent read in one round is replayed in the next; today a hostile comment must be met again on every run.
- **A reviewer keeps its earlier verdict**, which is the continuity it was asked for and also an anchor: the independent second look is lost on the same object.
- **A branch key is long-lived.** Every failure on one default branch shares one session, across unrelated fixes.
- **A lost session is silent.** A pruned, renamed or deleted one is created again, and nobody is told the context was dropped.
- **The operator's own resume can race the gate.** If a person continues the same title while a wake is live, two processes append to one transcript; `021` sees only the runs the service started.
- **The reason stays the whole instruction.** Resuming changes what a run remembers, not what it is told, so a wake that is wrong is still wrong.
