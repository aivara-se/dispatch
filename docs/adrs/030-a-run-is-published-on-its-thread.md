# 030 - A Run Is Published on Its Thread

## Context

The service starts a child process and forgets it ([`003`](003-starting-a-run.md)). A line in the audit says a wake happened ([`005`](005-the-audit-record.md)), and that line is on the operator's host. Nobody on the thread sees anything: the author waiting for a review, the reviewer waiting for a fix, the operator asking who is working on what.

[`028`](028-the-service-is-a-github-app.md) lets the service write on a thread. It does not say what a run looks like there. A comment per transition is two comments per wake and forty on a pull request that reaches the cap, and every created comment notifies every watcher. The service knows three facts about a run — it started, it exited, and the exit status — and nothing about the work between them.

## Decision

**One status comment per thread, written by the app, edited as its runs change.**

- **A row per agent**, for each run this process has seen on the object: its avatar, its login as a link, the reason phrase of its last wake, the state, and the time.
- **The states:** `started`, `held` behind a live run, `finished` with exit zero, `failed` with a non-zero exit, `not started` when the service could not launch it, and `recovered` when the boot reconcile started it.
- **Only a thread gets a card.** A pull request and an issue carry a number to comment on; a default-branch run has none. A red `main` ([`014`](014-a-red-main-wakes-its-pusher.md)) wakes its pusher and publishes nothing.
- **The login is a link, never an `@` mention.** The card names the agents it reports and wakes none of them.
- **Found by a marker**, `<!-- dispatch:status -->`, in the first line of its own body. A card the service cannot find is created, never guessed at.
- **Edits are coalesced** to one per object per five seconds, and the last state wins.
- **Times are absolute**, and the card carries the time of its last event. It claims nothing about the minutes since.
- **The card is derived.** The audit stays the record of whether a wake happened. A failed edit is logged and not retried, and never fails a wake.

## Consequences

- **Editing notifies nobody.** The card is quiet by design; the notification is [`028`](028-the-service-is-a-github-app.md)'s separate comment, posted when the service stops waking on a thread.
- **A crash leaves a row saying `started`, and a restart drops it.** The timestamp is the tell, and there is no sweep at boot. The map — object to comment, agent to last reason — dies with the process. The marker finds the comment, and the next edit writes the body from the rows the new process holds. A row comes back when that agent runs again, and the audit keeps the runs before.
- **Avatars decorate.** The login is in the same row, so a blocked image costs nothing.
- **The card says what the service did, not what the agent is doing.** A run that edits three repositories has one row, on one thread.
