# 004 - Starting a Run

## Context

`SYSTEMS.md` (*Starting a run*) fixes the requirement: start one bounded run of the named agent's own profile, hand it the reason, and know while it is running. The service is one Go process on the operator's host, beside the Hermes home, and an agent is a Hermes profile on that host. What is unrecorded is the mechanism, and what each candidate costs.

## Decision

**The child process is the mechanism.** The runner launches `hermes --profile <bot> chat -q "<reason>"` as a child of the service and tracks it: running, exited, its exit code and its last output. The run is the profile's own — its prompt, its skills, its memory and its delivery come from the profile — and the dispatcher adds only the reason.

- **One run per agent.** The runner starts no second run for a profile that already has one, and its own run table is what it reconciles against after a restart ([008](008-the-service-acts-on-the-board.md)).
- **The reason is the run's only input.** It says *why now*, and the agent re-reads the board and the pull request. That is what makes a wake that is early, late or duplicated harmless, and it is why the service never sends the work itself. What may be in it is [009](009-the-reason-carries-no-third-party-text.md).

## Consequences

- The service owns a process tree it did not write: it reaps the child, bounds its lifetime (provisional: thirty minutes, which the first live run freezes), and decides what a signal means for it. A run is a resource, and a run that dies is an exit code rather than silence.
- *Started* and *still running* are facts the service holds rather than infers, so one run per agent can be enforced while the service is up. What no child process shows is the run's content: the agent's own channel carries that, so the audit log records that a run started and never what became of it.
- The command line is a contract with another program: the global `--profile`/`-p` and the `chat -q` subcommand option are the Hermes CLI's, and a `-q` invocation on non-TTY stdio answers and exits, which is what a child process gets (Hermes Agent, *CLI Commands Reference*, read 2026-10-12). A Hermes upgrade can change it, and nothing in this repository's tests covers it; it is checked by hand on the host.
- A wake that arrives while an agent's run is in flight does not queue. The rule is one run per agent, and nothing waits its turn; what the service does instead is look again when that run ends ([008](008-the-service-acts-on-the-board.md)), so the card the dropped wake concerned is not left behind.
