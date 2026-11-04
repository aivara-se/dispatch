# Dispatch: product

## Problem

One person runs a small fleet of agents on GitHub. A review request, an assignment, a failing check: each reaches its agent only if the person relays it. The person is the router, and the event that arrives while they sleep is lost. The loss is silent.

GitHub posts to Dispatch when something happens in the organisation. Dispatch verifies the delivery and wakes the agent the event concerns, saying why now and nothing else. The operator installs it on their host; it is not a bot on GitHub.

## Scope

**In** — six flows: the review loop; an assignment; a failing check; a mention; a dismissed review; a draft marked ready. One run at a time per agent and object, and a wake that resumes that object's session. An audit line for every delivery. It acts on every repository in the organisation.

**Not this version** — the board and its cards; team reviewers; a retry queue; counts that survive a restart; any GitHub API call; metrics (#175).

**Never** — write to GitHub; read a body for anything but a login; log a payload body or a secret; wake the operator; carry third-party text into a reason; drop a wake.

## Behaviour

- A review request wakes the requested reviewer; a second request wakes it again.
- A review that requests changes wakes the author; an approval that lands nothing wakes the author.
- An approval that lands the change, and a review that only comments, wake nobody.
- A round is a review that requests changes. Ten rounds per pull request; an approval spends none.
- An assignment on an issue or a pull request wakes the assignee; no board is read.
- An agent is never woken for its own assignment, and neither is an unconfigured login.
- A failing check wakes the pusher, at most five times per pull request and five per branch.
- A passing, cancelled, timed-out or skipped run wakes nobody, and neither does a team reviewer.
- A mention wakes each configured login it names, once, from an owner, member or collaborator.
- A mention matches an exact login, case-insensitively: `@x.` matches and `@x-2` does not.
- A mention inside a code block or a quotation still matches.
- A dismissal on an open, unmerged, non-draft pull request wakes its author.
- A draft marked ready wakes the reviewers still requested on it; a team is never resolved.
- One run at a time per agent and object; a delivery behind it is held, not dropped.
- That run's exit starts one follow-up carrying the latest held reason.
- No agent is woken for a delivery it made, except the pusher of a failing run.
- The reason is identifiers and the service's own words, never text a third party wrote.

## Autonomy

The agent decides anything these documents and the ADRs do not fix: the code inside a part, its names, its tests, and the wording of the audit line. It asks first for a dependency, a configuration field, a webhook subscription, or a change to the flow.

## Vision

One process on the operator's own host, with no database and no credential of its own. It may learn new events. It may never write to GitHub, hold durable state, or become a bot that speaks.

## Acceptance

- Each flow wakes its agent, and nobody else.
- A changes request and an unarmed approval wake the author.
- Only a failing run's pusher wakes for its own delivery.
- Behind a live run, a delivery is held; one follow-up starts.
- Ten rounds; five failures per pull request and branch.
- The audit explains every silent delivery.
- Dispatch writes nothing to GitHub.
