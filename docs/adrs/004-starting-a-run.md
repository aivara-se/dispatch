# 004 - Starting a Run

## Context

`SYSTEMS.md` (*Starting a run*) fixes the requirement: start one bounded run of the named agent's own profile, hand it the reason, and know while it is running. The service is one Go process on the operator's host, beside the Hermes home, and an agent is a Hermes profile on that host. What is unrecorded is the mechanism, and what each candidate costs.

## Decision

**The child process is the mechanism.** The runner launches `hermes --profile <bot> chat -q "<reason>"` as a child of the service and tracks it: running, exited, its exit code and its last output. The run is the profile's own — its prompt, its skills, its memory and its delivery come from the profile — and the dispatcher adds only the reason.

- **One run per agent.** The runner starts no second run for a profile that already has one, and its own run table is what it reconciles against after a restart ([008](008-the-service-acts-on-the-board.md)).
- **The gateway is the fallback.** Where a profile's gateway must own the run and its delivery, the same wake is a signed POST to that profile's route — `/webhooks/<route>`, or `/p/<profile>/webhooks/<route>` where the gateway multiplexes profiles — signed with `X-Hub-Signature-256` in the form GitHub uses. The routes, the signature header and its verifier are the gateway's own, read from its handler (`NousResearch/hermes-agent`, `gateway/platforms/webhook.py`, read 2026-10-12); the answers it gives a wake are its own too, and are not the set in [006](006-idempotency-and-the-answer.md). The run's lifetime is then the gateway's, and the service tracks it through the board rather than through a child process.
- **The reason is the run's only input.** It says *why now*, and the agent re-reads the board and the pull request. That is what makes a wake that is early, late or duplicated harmless, and it is why the service never sends the work itself. What may be in it is [009](009-the-reason-carries-no-third-party-text.md).

## Consequences

- The service owns a process tree it did not write: it reaps the child, bounds its lifetime, and decides what a signal means for it. A run is a resource, and a run that dies is an exit code rather than silence.
- *Started* and *still running* are facts the service holds rather than infers, so one run per agent can be enforced while the service is up. What no child process shows is the run's content: the agent's own delivery carries that through the gateway, so the audit log records that a run started and never what became of it.
- The command line is a contract with another program: the global `--profile`/`-p` and the `chat -q` subcommand option are the Hermes CLI's, and a `-q` invocation on non-TTY stdio answers and exits, which is what a child process gets (Hermes Agent, *CLI Commands Reference*, read 2026-10-12). A Hermes upgrade can change it, and nothing in this repository's tests covers it; it is checked by hand on the host.
- The fallback costs a secret, a route and a signature per profile, and it gives up the exit code: liveness becomes an inference from the board, so a gateway-owned run that dies is noticed late — by its card not moving — rather than by the runner.
- A wake that arrives while an agent's run is in flight does not queue. The rule is one run per agent, and nothing waits its turn; what the service does instead is look again when that run ends ([008](008-the-service-acts-on-the-board.md)), so the card the dropped wake concerned is not left behind.

## Rejected

- **A command string in the configuration** — `command: "hermes --profile {bot} chat -q {reason}"`. Rejected: it puts a configuration value on the service's exec path and interpolates text from a payload into a shell. The profile is configuration; the argv is code.
- **The agent in-process**, linking the Hermes runtime into the service. Rejected: the agent is a separate program with its own lifetime, credentials and delivery, and a crashing service would take its runs with it — the opposite of a board that outlives the service.
- **The gateway as the only mechanism.** Rejected: it makes every profile's gateway a prerequisite, and the loopback child is what lets one host wake a profile that has none.
- **A wake that carries the task** — the whole instruction rather than the reason. Rejected: a late, repeated or out-of-order delivery would then do the work twice or in the wrong order, and a card edited after the wake would disagree with the instruction that woke it.
- **A queue or a job runner** — a `systemd --user` unit or a scheduler per run. Rejected: a second source of truth about what is running, where the board's `Started` column already is one.
