# 003 - Starting a Run

## Context

The receiver turns a delivery into the agents to wake ([002](002-github-webhook-ingestion.md)), and something must start an agent. The service is one Go process on the operator's host, beside the Hermes home; an agent is a Hermes profile on that host. What needs recording is the mechanism that starts the run, and what the choice rules out. For the one flow v1 carries — pull request reviews ([PRODUCT](../PRODUCT.md)) — a wake must not be delayed or dropped.

## Decision

**A child process.** A wake starts the named profile's own agent as a child of the service: `hermes --profile <profile> chat -q "<reason>"`. The run is the profile's own — its prompt, skills, memory and delivery come from the profile — and the service adds only the reason.

- **No serialisation.** A review request always starts a run, whatever else the agent is doing. The service keeps no record of runs in flight and never holds a wake back, so an agent may have more than one run at once, and two requests for one reviewer start two runs over the same pull request. A wake is a reason the agent re-reads, so a repeated run reviews again and is harmless; withholding a run is the one way to delay a review, and v1 does not.
- **The run is not tracked.** No run table, no exit-code bookkeeping, no lifetime bound: nothing about a run outlives the request that started it. The child is reaped so it does not linger as a zombie, and that is the whole of the service's interest in it — what became of the run is the agent's own channel's business.
- **The binary is named by an absolute path.** A systemd unit's `PATH` is minimal, so the bare `hermes` resolves in a shell and not in the unit; the path is configuration. The argv itself is a contract with another program: `--profile`/`-p` and the `chat -q` subcommand are the Hermes CLI's, and a `-q` run on non-TTY stdio answers and exits.

## Consequences

- The service owns no process tree it did not write: it starts a child and forgets it. Nothing is reconciled at boot, because nothing is remembered.
- A burst of deliveries for one agent starts a burst of runs. That is the trade — the flow is reviews, where a repeated review costs tokens and not correctness — and it is what keeps a busy agent from delaying a review.
- The command line is a contract this repository's tests cannot pin: an upgrade that moves it breaks the wake, and it is checked by hand on the host.
