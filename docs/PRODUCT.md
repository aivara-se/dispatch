# Dispatch: product

GitHub posts to `dispatch` when something happens in the `aivara-se` organisation. It verifies the delivery and, for the one flow it carries, wakes the agent the event concerns. The operator of a small fleet — one person, a few bots — installs it, trusts it, and reads its log when a wake is missed. The service does not do the work.

v1 carries a single flow: pull request reviews.

A wake is a **reason, not an instruction**: the service says *why now*, and the agent re-reads the pull request and decides what to do. A wake may therefore be early, late, duplicated or out of order and the outcome is the same.

## Configuration

The operator lists the agents the service knows, each with its GitHub login and the local profile that login runs as. A login that is not on the list — a human reviewer, say — wakes nobody. The service is configured from one file and acts on every repository under `aivara-se`.

## The flow: pull request reviews

When a pull request asks an agent to review it, the service wakes that agent and the agent reviews the pull request. A pull request asks when:

- an agent is added as a reviewer; or
- an agent's review is requested again.

Every agent reviews. A review is not a card and never takes a developer's slot, and a reviewer never pushes to a peer's branch: the author applies every change.

## What it never does

- Never starts an agent the state does not call for, and never the operator. No agent is woken for the event it made.
- Never holds a wake back or queues one: a review request starts a run even if the agent is already running one.
- Never logs a payload body or a secret value.
- Never carries text a third party wrote into a run: the reason is identifiers and the service's own words.
- Never orders events, and never depends on their order.

## What v1 does not do

- Not the board. It carries no card flow: it does not read cards, move them or assign them.
- Not a deployment step. A merge is the repository's own pipeline's business.

## Acceptance

v1 is done when each of these holds, and each is checkable by the operator.

- An agent added as a reviewer on a pull request wakes and reviews it.
- An agent whose review is requested again wakes and reviews it.
- No agent is woken for the delivery it made.
- A redelivered delivery wakes nobody, and the audit log shows it was seen.
- The audit log answers, for any delivery, whether an agent was started and, when not, why.
