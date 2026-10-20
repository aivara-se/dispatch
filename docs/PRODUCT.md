# Dispatch: product

GitHub posts to `dispatch` when something happens in the `aivara-se` organisation. It verifies the delivery and wakes the agent the event concerns: the reviewer a pull request asks for, or the author whose review requests changes. The operator of a small fleet — one person, a few bots — installs it, trusts it, and reads its log when a wake is missed. The service does not do the work.

v1.1 carries one flow: the review and update loop on a pull request.

A wake is a **reason, not an instruction**: the service says *why now*, and the agent re-reads the pull request and decides what to do. A wake may therefore be early, late, duplicated or out of order and the outcome is the same.

## Configuration

The operator lists the agents the service knows, each with its GitHub login and the local profile that login runs as. A login that is not on the list — a human reviewer, say — wakes nobody. The service is configured from one file and acts on every repository under `aivara-se`.

## The flow: review and update

When a pull request asks an agent to review it, the service wakes that agent. A pull request asks when an agent is added as a reviewer, or an agent's review is requested again.

When a review requests changes, the service wakes the pull request's author. The author updates the branch and asks for the review again, which wakes the reviewer. That is the loop. A review that approves or only comments wakes nobody.

The loop has no cap in v1.1: it ends when a review approves, when the author stops asking for one, or when the operator steps in.

Every agent reviews. A review is not a card and never takes a developer's slot, and a reviewer never pushes to a peer's branch: the author applies every change.

## What it never does

- Never starts an agent the state does not call for, and never the operator. No agent is woken for the event it made.
- Never writes to GitHub: no label, no comment, no merge, no push.
- Never holds a wake back or queues one: a review request starts a run even if the agent is already running one.
- Never logs a payload body or a secret value.
- Never carries text a third party wrote into a run: the reason is identifiers and the service's own words.
- Never orders events, and never depends on their order.

## What v1.1 does not do

- Not the board. It carries no card flow: it does not read cards, move them or assign them.
- Not a reviewer. It never reads a diff and never decides whether a pull request is good.
- Not a cap. It counts nothing and stops no loop.
- Not a deployment step. A merge is the repository's own pipeline's business.

## Acceptance

v1.1 is done when each of these holds, and each is checkable by the operator.

- An agent added as a reviewer on a pull request wakes and reviews it.
- An agent whose review is requested again wakes and reviews it.
- A review that requests changes wakes the pull request's author, and only the author.
- A review that approves or only comments wakes nobody.
- No agent is woken for the delivery it made.
- A redelivered delivery wakes nobody, and the audit log shows it was seen.
- The audit log answers, for any delivery, whether an agent was started and, when not, why.
