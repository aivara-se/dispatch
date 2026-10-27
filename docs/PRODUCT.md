# Dispatch: product

GitHub posts to `dispatch` when something happens in the `aivara-se` organisation. It verifies the delivery and wakes the agent the event concerns: the reviewer a pull request asks for, the author whose review requests changes or whose change is approved but not armed, the agent an assignment or a comment names, the pusher of a pull request or of the default branch whose checks failed, the author whose review was dismissed, or the reviewers of a draft that is marked ready. The operator of a small fleet — one person, a few bots — installs it, trusts it, and reads its log when a wake is missed. The service does not do the work.

v1.15 carries six flows: the review and update loop on a pull request, an assignment on an issue or on a pull request, a failing check on a pull request or on the default branch, a mention in a comment, in a review's body or in a review comment, a draft that becomes ready for review, and a review that is dismissed. One agent runs one thing at a time on one pull request or one branch ([`021`](adrs/021-one-run-per-pull-request-per-agent.md)).

A wake is a **reason, not an instruction**: the service says *why now*, and the agent re-reads the pull request or the issue and decides what to do. A wake may therefore be early, late, duplicated or out of order and the outcome is the same.

## Configuration

The operator lists the agents the service knows, each with its GitHub login and the local profile that login runs as. A login that is not on the list — a human reviewer, say — wakes nobody. The service is configured from one file and acts on every repository under `aivara-se`.

## The flow: review and update

When a pull request asks an agent to review it, the service wakes that agent. A pull request asks when an agent is added as a reviewer, or an agent's review is requested again.

When a review requests changes, the service wakes the pull request's author. The author updates the branch and asks for the review again, which wakes the reviewer. That is the loop. An approval lands the change and wakes nobody; an approval that lands nothing — on a pull request that is open, not a draft and not armed — wakes its author ([`012`](adrs/012-an-approval-that-lands-nothing-wakes-the-author.md)). A review that only comments wakes nobody. An approval spends no round, and the cap does not refuse one.

The loop is capped at ten rounds. A round is a review that requests changes. When a pull request reaches ten, a further review request or changes-requested review on it wakes nobody, so a disagreement cannot run forever. The count belongs to the pull request and lives in memory.

Every agent reviews. A review is not a card and never takes a developer's slot, and a reviewer never pushes to a peer's branch: the author applies every change.

## The flow: assignment

When an issue or a pull request is assigned to an agent, the service wakes that agent. The assignment is the whole trigger: the service reads no board, so an issue that is not a card is still an issue, and every repository in the organisation is in scope.

The agent that made the assignment is never woken for it, so an agent cannot start a run by taking an issue or a pull request itself. A login the configuration does not carry — the operator's, a bot's, a human's — wakes nobody. An issue or a pull request created with an assignee is an assignment like any other: GitHub delivers `assigned` after `opened`, and the wake is the same one. The assignment adds no round and never consults the cap.

## The flow: a failing check

When a workflow run on a pull request completes as `failure`, the service wakes the account that triggered the run. The payload names no pull request author, so the pusher stands in for one: in this organisation the author is the only one who pushes to a pull request's own branch, and a reviewer never does.

This is the one delivery that wakes its own actor. A push does not tell the pusher that the checks failed, so the fact is news to the only agent that can act on it.

A run that succeeded, was cancelled, timed out or was skipped wakes nobody. So does a failing run that names no pull request and runs on neither a pull request's branch nor the default one, and a pusher the configuration does not carry. A failing run that names several pull requests wakes once, with the first.

A run that names no pull request and fails on the default branch — a `push` run — wakes the account that pushed it, with the branch in the reason instead of a number ([`014`](adrs/014-a-red-main-wakes-its-pusher.md)). The branch is read from the payload, not configured. A failing run on the default branch that is not a `push` — a schedule, a dispatch, another workflow — wakes nobody, so a nightly that goes red stays the operator's to see.

The cap here is five wakes per pull request, and five per repository and branch, held apart from the review's ten and from each other: a failed check is not a review round, and a flaky test must not spend the review budget. Each count lives in memory, is checked before the wake and advances only when a run starts. At five, a further failure wakes nobody, so a break nobody can fix stops reaching the agent and becomes the operator's.

## The flow: a mention

When a comment on an issue or a pull request is created, a review is submitted, or an inline review comment is created, the service wakes each configured agent whose exact `@login` the text names ([`016`](adrs/016-a-mention-reads-the-reviews-own-surfaces.md)). The match is case-insensitive and bounded: `@thani-sh-mama.` matches, `@thani-sh-mama-2` and `x@thani-sh-mama` do not. An agent named three times is woken once, however often it is named.

Only the repository's owner, a member of the organisation or a collaborator may summon an agent. A comment from a contributor, a first-time commenter or a stranger wakes nobody, whatever it says. The body is read only to find a configured login in it, and no text from it reaches the reason or the log.

A review that requests changes has two targets in one delivery: the author it wakes ([`006`](adrs/006-the-review-loop.md)), and any configured login its body names. Both start their own run with their own reason and are recorded on one line.

An agent that mentions itself is not woken. The mention adds no round and consults neither cap. An edited or a deleted comment wakes nobody, and neither does an edited or a deleted review.

## The flow: a review is dismissed

When a review on an open, unmerged, non-draft pull request is dismissed, the service wakes the pull request's author ([`017`](adrs/017-a-dismissed-review-wakes-the-author.md)). An approval can stop existing — a maintainer dismisses it, or GitHub dismisses a stale one when commits arrive on the branch — and the change then cannot land, so the author is told at the moment they still have the branch in hand.

A dismissal on a draft, on a closed pull request or on one already merged wakes nobody. The delivery's own sender is not woken, and the dismissal neither advances nor consults either cap.

## The flow: a draft becomes ready

When a draft pull request is marked ready for review, the service wakes every configured agent still requested to review it. A requested team is not resolved, which would take a GitHub call, so it wakes nobody; the same is true of a review requested from a team. The account that marked the draft ready is never woken, and a draft marked ready before anyone was requested wakes nobody.

This is the transition from probe to candidate: the reviewers were woken when the draft opened, when there was nothing to merge, and this is the wake that says the change may land. The delivery adds no round and consults neither cap.

## One run at a time

An agent may have several runs at once, but not two over the same object: one pull request or one branch is worked by one agent at a time ([`021`](adrs/021-one-run-per-pull-request-per-agent.md)). A delivery that lands while that agent's run on that object is live starts nothing. It is held: when the run exits, one follow-up starts carrying the reason of the most recent delivery held for it, so a burst becomes one run, and the audit pairs the held line with the follow-up's by the delivery id. A held delivery replaces an earlier held one and is traced the same way. Different pull requests do not wait on each other, and neither do two agents on one pull request.

The wait buys a whole review instead of half of one. Two runs over one head published two `CHANGES_REQUESTED` reviews 40 s apart, each carrying half the findings, and neither saw the other. What it costs is a review request waiting for the review already running, and a run that never exits holding its key until the process restarts.

## What it never does

- Never starts an agent the delivery and the configuration do not call for, and never the operator. No agent is woken for the event it made, with one exception: the pusher of a failing run, who is the agent the failure is news to.
- Never writes to GitHub: no label, no comment, no merge, no push.
- Never drops a wake: a review request, an assignment, a failed check, a mention, a dismissal or a draft marked ready starts a run, or is held for the agent's run on the same object and starts when it exits. Nothing is queued beyond that one held delivery.
- Never logs a payload body or a secret value.
- Never carries text a third party wrote into a run: the reason is identifiers and the service's own words. A comment, a review body or a review comment is read only to find a configured login in it. The one branch in a reason is the red-main ref, which a number would be if the run named a pull request.
- Never orders events, and never depends on their order.

## What v1.15 does not do

- Not the board. It carries no card flow: it does not read cards, move them or assign them. An issue's column, its links and whether it is a card at all are invisible to it.
- Not a watcher. It wakes the agent an assignment names and then forgets the issue or the pull request: a reassignment away, a close or a later edit reaches nobody.
- Not a reviewer. It never reads a diff and never decides whether a pull request is good.
- Not a fixer. It wakes the pusher of a red pull request or default branch and never reads the failing run's output, so the reason says a check failed and not which.
- Not a mention parser. It matches an exact login and does not read Markdown, so a mention inside a code block, a quotation or a sentence about an agent still matches.
- Not a resolver of teams. A review requested from a team wakes nobody, whatever the event.
- Not a run's biographer. The audit records that a run exited non-zero and never what it said.
- Not a durable cap. The counts die with the process, and a boot line marks where.
- Not a deployment step. A merge is the repository's own pipeline's business.

## Edge cases v1.15 leaves unhandled

Known, and deliberately not handled in this version. Each is a wake that does not happen, or a run that happens twice.

- **Several assignees in one change.** Only the payload's `assignee` is read, never its `assignees` array; whether GitHub reports such a change as one delivery per assignee is unrecorded here.
- **An issue or a pull request unassigned, or reassigned away.** The agent that held it is not woken and is not told.
- **An assignment made outside GitHub** — on a local board, or by a sync from another system. No delivery, no wake.
- **A review requested from a team.** The delivery names a team, not a login, so nobody is woken.
- **An approval on a draft, on a closed or merged pull request, or on one armed for auto-merge.** Nobody is woken, which is the case an unarmed approval on an open pull request is not.
- **A review edited to approved.** Only a submitted review acts, so the edit wakes nobody.
- **A mention in a code block, a quotation or a sentence about an agent.** It matches: the service does not read Markdown, and a parser would be a larger thing than this service.
- **A mention from a bot, a contributor or a first-time commenter.** The association is not the owner's, a member's or a collaborator's, so it wakes nobody even when the login it names is configured.
- **A mention in a commit comment or in a pull request's or an issue's description.** A comment, a submitted review and an inline review comment are read; nothing else reaches the matcher.
- **A review and its inline comments are separate deliveries.** A review naming an agent in both its body and a comment starts two runs, and several comments naming the same agent start one run each.
- **Two approvals dismissed by one push.** The delivery id is the unit of dedupe and the review is the unit of a dismissal, so the author is woken twice.
- **A team reviewer on a draft marked ready.** A team is not resolved, so it wakes nobody while the pull request's other reviewers are woken.
- **Toggling a draft back to ready.** Each return to ready is a new delivery, so the reviewers are woken again; only the delivery-id cache stops a redelivery.
- **The operator pushes to an agent's branch.** The run's actor is the operator, so nobody is woken and the agent is not told its checks failed.
- **A pull request from a fork.** Its runs are triggered by the forker, who is not a configured login, so a failure wakes nobody.
- **A failing run on a branch that is neither a pull request's nor the default branch.** The payload names no number and the branch is not the default one, so nobody is woken.
- **A failing run on the default branch that is not a push.** A schedule, a dispatch or another workflow wakes nobody.
- **A flaky check.** It spends the failure budget as surely as a real break; the count does not know the difference.
- **A failing run that names several pull requests.** One run starts, carrying the first pull request's number; the others are named only when a later run fails.
- **A run that never exits.** Its key stays held until the process restarts, so that agent's wakes for that object stop arriving.
- **A crash between a held delivery and its follow-up.** The held line is the trace of a wake that did not happen.
- **A restart.** A boot line marks it and every count starts over, so a redelivery inside the lost hour can wake the agent twice.

## Acceptance

v1.15 is done when each of these holds, and each is checkable by the operator.

- An agent added as a reviewer on a pull request wakes and reviews it.
- An agent whose review is requested again wakes and reviews it.
- A review that requests changes wakes the pull request's author, and only the author.
- A review that only comments wakes nobody, and an approval that lands the change wakes nobody.
- An approval on a pull request that is open, not a draft and not armed wakes its author, and only its author.
- An approval on a draft, on a closed pull request or on one armed for auto-merge wakes nobody, and the audit log says so.
- An approval spends no round, and the ten-round cap does not refuse one.
- A review requested from a team wakes nobody, and the audit log says so.
- An issue assigned to a configured agent wakes that agent, and nobody else.
- A pull request assigned to a configured agent wakes that agent, and nobody else.
- An issue or a pull request assigned to a login the configuration does not carry wakes nobody, and the audit log says so.
- An agent that assigns an issue or a pull request to itself is not woken.
- A check that fails on a pull request wakes the account that pushed it, and only that account.
- A check that succeeds, is cancelled, times out or is skipped wakes nobody, and the audit log says so.
- A failing run on the default branch that names no pull request wakes the account that pushed it, and a failing run on another such branch wakes nobody.
- A failing run that names several pull requests starts one run.
- A failing check on a login the configuration does not carry wakes nobody, and the audit log says so.
- A pull request stops waking its pusher once five failed checks have woken someone, and a default branch stops too, counted on its own.
- A failed check does not spend the review's ten rounds, and a review that requests changes does not spend the five.
- A run that exits non-zero appends a line of its own carrying the delivery, the profile and the exit status, and a run that exits zero appends none.
- Each boot appends one line carrying the moment, the build, the listen address and the number of configured agents.
- An issue delivery that is opened, edited, labelled or unassigned wakes nobody, and the audit log says so.
- A pull request delivery that is opened, edited, labelled or unassigned wakes nobody, and the audit log says so.
- A comment from the owner, a member or a collaborator naming a configured agent wakes it, once, however often the comment names it.
- One comment naming several configured agents wakes each of them, and the audit log records them on one line.
- A mention in a comment from a contributor, a first-time commenter or a stranger wakes nobody, and the audit log says why.
- An `@login` in a review's body or in an inline review comment wakes the agent it names, under the same guard and with the same reason.
- A review that requests changes wakes its author and every login its body names, on one line, each with its own reason.
- An agent that mentions itself is not woken, and an edited or a deleted comment wakes nobody.
- The ten-round cap does not refuse a mention.
- A dismissal on an open, unmerged, non-draft pull request wakes its author, and only its author.
- A dismissal on a draft, on a closed pull request or on a merged one wakes nobody, and the audit log says so.
- A dismissal advances no round and consults neither cap.
- A draft marked ready wakes every configured reviewer still requested on it, and nobody else.
- A draft marked ready before any reviewer was requested wakes nobody, and the audit log says so.
- A draft toggled back to ready wakes its reviewers again.
- No agent is woken for the delivery it made, except the pusher of a failing run, on a pull request or on the default branch.
- A redelivered delivery wakes nobody, and the audit log shows it was seen.
- The audit log answers, for any delivery, whether an agent was started and, when not, why.
- A pull request stops waking anyone once it has had ten reviews that request changes.
- A delivery that lands while its agent's run on the same pull request is live starts no run, and its line says it was held and the key it waits on.
- When that run exits, one follow-up run starts carrying the held delivery's reason, and its line carries the held delivery's id.
- A second delivery held for the same key replaces the first, and the one follow-up carries the latest reason.
- Two pull requests of one agent run at once, and so do two agents on one pull request.
- A run that exits with nothing held behind it starts nothing, and the key is free for the next delivery.

## Changelog

- **v1.0** — a review request on a pull request wakes the reviewer that was asked.
- **v1.1** — a review that requests changes wakes the pull request's author, closing the review and update loop.
- **v1.2** — the loop is capped at ten rounds, so a disagreement cannot spend runs forever.
- **v1.3** — assigning an issue wakes the agent it was assigned to.
- **v1.4** — assigning a pull request wakes the agent it was assigned to, on the same rules as an issue's assignment.
- **v1.5** — a failing check wakes the account that pushed the pull request, at most five times per pull request — the one wake aimed at the delivery's own actor.
- **v1.6** — an exact `@login` in a comment from the owner, a member or a collaborator wakes that agent, on an issue or on a pull request, once per agent per comment ([`011`](adrs/011-a-mention-wakes-the-agent-it-names.md)).
- **v1.7** — an approval on a pull request that is not armed wakes its author, because the approval is the merge and an unarmed change lands nothing ([`012`](adrs/012-an-approval-that-lands-nothing-wakes-the-author.md)).
- **v1.8** — a draft marked ready wakes the reviewers still requested on it, because that is the moment a probe becomes a candidate ([`013`](adrs/013-a-draft-marked-ready-wakes-its-reviewers.md)).
- **v1.9** — a failing run on the default branch wakes the account that pushed it, so a red `main` is not the one failure nobody hears about ([`014`](adrs/014-a-red-main-wakes-its-pusher.md)).
- **v1.10** — the audit carries a run that ended non-zero, so a wake that was started and died is visible ([`015`](adrs/015-the-audit-records-a-runs-exit.md)).
- **v1.11** — a mention is read in a review's body and in a review comment, where the review conversation is written ([`016`](adrs/016-a-mention-reads-the-reviews-own-surfaces.md)).
- **v1.12** — a dismissed review wakes the pull request's author, because the approval the change was waiting on is gone ([`017`](adrs/017-a-dismissed-review-wakes-the-author.md)).
- **v1.13** — the service's own boot reaches the audit, so a restart is visible and the moment a count was lost is marked ([`018`](adrs/018-the-services-boot-reaches-the-audit.md)).
- **v1.14** — every wake's session is tagged `dispatch`, so the service's own runs are one filter in the session list ([`019`](adrs/019-every-run-is-tagged-dispatch.md)).
- **v1.15** — one run at a time per pull request or branch and agent: a delivery that lands on a key whose run is live is held, and its exit starts one follow-up, so two partial reviews of one head cannot be published ([`021`](adrs/021-one-run-per-pull-request-per-agent.md)).

## Roadmap

Not built. Each line is decided and unbuilt; the present tense of this document still describes v1.15.

- **v1.16** — a wake resumes the session it left for the same pull request or branch, so an agent answering a later round keeps the reasoning of the rounds before it instead of re-deriving it ([`022`](adrs/022-a-wake-resumes-its-objects-session.md)).
