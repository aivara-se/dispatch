# Dispatch: product

GitHub posts to `dispatch` when something happens in the `aivara-se` organisation. It verifies the delivery and wakes the agent the event concerns: the reviewer a pull request asks for, the author whose review requests changes, the agent an assignment names, or the pusher of a pull request whose checks failed. The operator of a small fleet — one person, a few bots — installs it, trusts it, and reads its log when a wake is missed. The service does not do the work.

v1.5 carries three flows: the review and update loop on a pull request, an assignment on an issue or on a pull request, and a failing check.

A wake is a **reason, not an instruction**: the service says *why now*, and the agent re-reads the pull request or the issue and decides what to do. A wake may therefore be early, late, duplicated or out of order and the outcome is the same.

## Configuration

The operator lists the agents the service knows, each with its GitHub login and the local profile that login runs as. A login that is not on the list — a human reviewer, say — wakes nobody. The service is configured from one file and acts on every repository under `aivara-se`.

## The flow: review and update

When a pull request asks an agent to review it, the service wakes that agent. A pull request asks when an agent is added as a reviewer, or an agent's review is requested again.

When a review requests changes, the service wakes the pull request's author. The author updates the branch and asks for the review again, which wakes the reviewer. That is the loop. A review that approves or only comments wakes nobody.

The loop is capped at ten rounds. A round is a review that requests changes. When a pull request reaches ten, a further review request or changes-requested review on it wakes nobody, so a disagreement cannot run forever. The count belongs to the pull request and lives in memory.

Every agent reviews. A review is not a card and never takes a developer's slot, and a reviewer never pushes to a peer's branch: the author applies every change.

## The flow: assignment

When an issue or a pull request is assigned to an agent, the service wakes that agent. The assignment is the whole trigger: the service reads no board, so an issue that is not a card is still an issue, and every repository in the organisation is in scope.

The agent that made the assignment is never woken for it, so an agent cannot start a run by taking an issue or a pull request itself. A login the configuration does not carry — the operator's, a bot's, a human's — wakes nobody. An issue or a pull request created with an assignee is an assignment like any other: GitHub delivers `assigned` after `opened`, and the wake is the same one. The assignment adds no round and never consults the cap.

## The flow: a failing check

When a workflow run on a pull request completes as `failure`, the service wakes the account that triggered the run. The payload names no pull request author, so the pusher stands in for one: in this organisation the author is the only one who pushes to a pull request's own branch, and a reviewer never does.

This is the one delivery that wakes its own actor. A push does not tell the pusher that the checks failed, so the fact is news to the only agent that can act on it.

A run that succeeded, was cancelled, timed out or was skipped wakes nobody. So does a failing run that names no pull request, and a pusher the configuration does not carry. A failing run that names several pull requests wakes once, with the first.

The cap here is five wakes per pull request, held apart from the review's ten: a failed check is not a review round, and a flaky test must not spend the review budget. The count belongs to the pull request, lives in memory, is checked before the wake and advances only when a run starts. At five, a further failure wakes nobody, so a break nobody can fix stops reaching the agent and becomes the operator's.

## What it never does

- Never starts an agent the delivery and the configuration do not call for, and never the operator. No agent is woken for the event it made, with one exception: the pusher of a pull request whose checks failed, who is the agent the failure is news to.
- Never writes to GitHub: no label, no comment, no merge, no push.
- Never holds a wake back or queues one: a review request, an assignment or a failed check starts a run even if the agent is already running one.
- Never logs a payload body or a secret value.
- Never carries text a third party wrote into a run: the reason is identifiers and the service's own words.
- Never orders events, and never depends on their order.

## What v1.5 does not do

- Not the board. It carries no card flow: it does not read cards, move them or assign them. An issue's column, its links and whether it is a card at all are invisible to it.
- Not a watcher. It wakes the agent an assignment names and then forgets the issue or the pull request: a reassignment away, a close or a later edit reaches nobody.
- Not a reviewer. It never reads a diff and never decides whether a pull request is good.
- Not a fixer. It wakes the pusher of a red pull request and never reads the failing run's output, so the reason says a check failed and not which.
- Not a durable cap. The counts die with the process.
- Not a deployment step. A merge is the repository's own pipeline's business.

## Edge cases v1.5 leaves unhandled

Known, and deliberately not handled in this version. Each is a wake that does not happen, or a run that happens twice.

- **Several assignees in one change.** Only the payload's `assignee` is read, never its `assignees` array; whether GitHub reports such a change as one delivery per assignee is unrecorded here.
- **An issue or a pull request unassigned, or reassigned away.** The agent that held it is not woken and is not told.
- **An assignment made outside GitHub** — on a local board, or by a sync from another system. No delivery, no wake.
- **The operator pushes to an agent's branch.** The run's actor is the operator, so nobody is woken and the agent is not told its checks failed.
- **A pull request from a fork.** Its runs are triggered by the forker, who is not a configured login, so a failure wakes nobody.
- **A failure on a branch with no pull request.** The payload names no number, so the run points at nothing and wakes nobody.
- **A flaky check.** It spends the failure budget as surely as a real break; the count does not know the difference.
- **A failing run that names several pull requests.** One run starts, carrying the first pull request's number; the others are named only when a later run fails.
- **An agent already running.** A wake starts a second run; nothing queues and nothing waits.
- **A restart.** A wake is not remembered beyond the delivery-id cache, so a redelivery inside the lost hour can wake the agent twice, and the counts start over.

## Acceptance

v1.5 is done when each of these holds, and each is checkable by the operator.

- An agent added as a reviewer on a pull request wakes and reviews it.
- An agent whose review is requested again wakes and reviews it.
- A review that requests changes wakes the pull request's author, and only the author.
- A review that approves or only comments wakes nobody.
- An issue assigned to a configured agent wakes that agent, and nobody else.
- A pull request assigned to a configured agent wakes that agent, and nobody else.
- An issue or a pull request assigned to a login the configuration does not carry wakes nobody, and the audit log says so.
- An agent that assigns an issue or a pull request to itself is not woken.
- A check that fails on a pull request wakes the account that pushed it, and only that account.
- A check that succeeds, is cancelled, times out or is skipped wakes nobody, and the audit log says so.
- A failing run that names no pull request wakes nobody, and the audit log says so.
- A failing run that names several pull requests starts one run.
- A failing check on a login the configuration does not carry wakes nobody, and the audit log says so.
- A pull request stops waking its pusher once five failed checks have woken someone, and the audit log says so.
- A failed check does not spend the review's ten rounds, and a review that requests changes does not spend the five.
- An issue delivery that is opened, edited, labelled or unassigned wakes nobody, and the audit log says so.
- A pull request delivery that is opened, edited, labelled or unassigned wakes nobody, and the audit log says so.
- No agent is woken for the delivery it made, except the pusher of a pull request whose checks failed.
- A redelivered delivery wakes nobody, and the audit log shows it was seen.
- The audit log answers, for any delivery, whether an agent was started and, when not, why.
- A pull request stops waking anyone once it has had ten reviews that request changes.

## Changelog

- **v1.0** — a review request on a pull request wakes the reviewer that was asked.
- **v1.1** — a review that requests changes wakes the pull request's author, closing the review and update loop.
- **v1.2** — the loop is capped at ten rounds, so a disagreement cannot spend runs forever.
- **v1.3** — assigning an issue wakes the agent it was assigned to.
- **v1.4** — assigning a pull request wakes the agent it was assigned to, on the same rules as an issue's assignment.
- **v1.5** — a failing check wakes the account that pushed the pull request, at most five times per pull request — the one wake aimed at the delivery's own actor.

## Roadmap

Decided and not yet built. Each version widens the set of deliveries the service acts on; a line moves into the changelog when its code lands.

- **v1.6** — an exact `@login` in a comment from a member wakes that agent, on an issue or a pull request ([`011`](adrs/011-a-mention-wakes-the-agent-it-names.md)).
