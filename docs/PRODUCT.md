# dispatcher: product

GitHub posts to `dispatcher` when something happens in the `aivara-se` organisation. It verifies the delivery, works out which agents the fact concerns, writes one field of the board, and starts their runs with the reason attached. The operator of a small fleet — one person, four bots, one board — installs it, trusts it, and reads its log when a wake is missed. The service does not do the work.

A wake is a **reason, not an instruction**: the service says *why now*, and the agent re-reads the board and the pull request and decides what to do. A wake may therefore be early, late, duplicated or out of order and the outcome is the same. The mechanisms behind it are the subject of the decisions in `docs/adrs/`.

## The board

One project, four columns. A card is added to the board by the operator or by an agent, and a pull request merges when the operator merges it: the two acts the service does not take.

- `Backlog` — the operator's inbox, and MaMa's.
- `Refined` — ready to build.
- `Started` — being built; a developer holds one card at a time, until its pull request merges.
- `Finished` — merged, and in most cases deployed.

A card is a GitHub issue and its column is the project's `Status` field. The board is the service's only state: it reads the board and writes one field of it, the assignee. Work that is not a card on the board is work the service cannot see.

## The agents

- **MaMa** refines: she checks a card is feasible, fills in what is missing, splits it when it is too big, links what blocks it, and moves it to `Refined`.
- **MeMe and MiMi** build, one card at a time, to a merged pull request.
- **MoMo** attacks and holds no card: it writes no product code, so `Refined` work is only ever assigned to MeMe or MiMi.
- **Every agent reviews.** A review is not a card and never takes a developer's slot, and a reviewer never pushes to a peer's branch: the author applies every change.

## The rule

One question, asked of every card every time the service looks: **what moves this card forward, who does it, and is it already happening?** Where the answer is an agent and nothing is in flight for it, the service writes the assignee and starts that agent's run.

- **`Backlog`** — MaMa refines it; she reads the links between cards and decides what is blocked.
- **`Refined`** — a developer holding no card builds it. With none free, the card waits and is looked at again when a run ends.
- **`Started`** — nothing while its run is in flight. With no run, its assignee, to finish or restart it.
- **A review requested on a pull request** — the named reviewer.
- **Changes requested on a pull request** — its author.
- **A pull request approved and mergeable** — the merge is the operator's. *How the service reaches the operator is not decided.*
- **A pull request merged** — MaMa, for the cards it closed and the blockers it cleared.
- **A pull request closed unmerged** — its author, to reopen or rework it.
- **A card whose next step is the operator's** — the operator again. *The same undecided route.*

The service looks in three places and nowhere else: when a delivery arrives, when a run it started ends, and once at boot. Nothing else fires it — there is no clock.

## What it never does

- Never starts an agent the state does not call for, and never the operator. The delivery's own actor is never in that delivery's wake set: an agent is not woken for the event it made, though a card it opened is still a card a look may wake it for.
- Never starts a second run for one agent.
- Never writes a field other than the assignee, and never moves a card.
- Never parses a body before the signature verifies, and never logs a payload body or a secret value.
- Never carries text a third party wrote into a run: the reason is identifiers and the service's own words.
- Never orders events, and never depends on their order.
- Never invents a queue: nothing about the service survives its own restart except the board and its log.

## What v1 does not do

- Not a scheduler. Nothing fires on a clock.
- Not a dependency resolver. MaMa reads the links between cards and works out what a `Finished` card unblocked; the service models no blockers.
- Not a board writer beyond the assignee, and not a second source of truth.
- Not a deployment step. A merge ends the card; what a merge deploys is the repository's own pipeline.

## Acceptance

v1 is done when each of these holds, and each is checkable by the operator.

- One card added to `Backlog`, and nothing else touched, reaches a merged pull request, and the service carries every wake on the way.
- A card already `Refined` when the service boots reaches a developer with no delivery required.
- A card added to `Backlog` is assigned to MaMa and her run starts within seconds (provisional; the first live delivery freezes the number).
- No card sits in `Started` with no run behind it: a run that ended without its card moving is looked at again.
- A review request wakes the named reviewer; a merge wakes MaMa; no agent is woken for the delivery it made. One look may wake more than one agent, because a look answers for every card it reads — and a card an agent opened is still a card, so a look wakes its assignee for it like any other.
- A redelivered delivery wakes nobody, and the audit log shows it was seen.
- A run the service could not start is dead-lettered rather than dropped.
- The audit log answers, for any delivery, whether an agent was started and, when not, why — and, for any card, what its runs cost.
