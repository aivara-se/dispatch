# 004 - Starting a Run

## Context

`SYSTEMS.md` (*Starting a run*) fixes the requirement: start one bounded run of the named agent's own profile, hand it the reason, and know while it is running. The service is one Go process on the operator's host, beside the Hermes home, and an agent is a Hermes profile on that host. What is unrecorded is the mechanism, and what each candidate costs.

## Decision

**The child process is the mechanism.** The runner launches `hermes --profile <bot> chat -q "<reason>"` as a child of the service and tracks it: running, exited, and its exit code. The run is the profile's own — its prompt, its skills, its memory and its delivery come from the profile — and the dispatcher adds only the reason.

- **One run per agent.** The runner starts no second run for a profile that already has one, and its own run table is what it reconciles against; what the table is, and what it holds at boot, is [008](008-the-service-acts-on-the-board.md)'s.
- **The reason is the run's only input.** It says *why now*, and the agent re-reads the board and the pull request. That is what makes a wake that is early, late or duplicated harmless, and it is why the service never sends the work itself. What may be in it is [009](009-the-reason-carries-no-third-party-text.md).

## Consequences

- The service owns a process tree it did not write: it reaps the child, bounds its lifetime (provisional: thirty minutes, which the first live run freezes), and decides what a signal means for it. A run is a resource, and a run that dies is an exit code rather than silence.
- **A child that outlives the service** is a run the runner no longer holds. A stop that cancels the boot's context — `SIGTERM`, and so a clean stop — kills every run's group through `exec.CommandContext`, and nothing is left behind. A stop that does not — `SIGKILL`, and so a crash or a failure restart — leaves each live child reparented and still leading its own group. The child is recognised as this service's own run by the run tag it was started with in its environment — the variable `DISPATCHER_RUN`, its value the tag the service mints for the run before the child starts — read beside the command line that names the Hermes binary the service launches, both from the process's own files (proc(5)). The tag is not the run's session: the profile's own session id does not exist until the child has started, so it is the child's to create and not the service's to record before it runs, which is why the value the service sets and reads is a tag it minted itself. The next boot, before its first look, kills each such group and waits for it to go, so the profile is free and the boot look's run for it is the only one; the interrupted run's work is abandoned and re-derived from the board, which a wake being a reason rather than an instruction makes harmless ([008](008-the-service-acts-on-the-board.md) names the run table and the restart cost).
- *Started* and *still running* are facts the service holds rather than infers, so one run per agent can be enforced while the service is up. What no child process shows is the run's content: the agent's own channel carries that, so the audit log records that a run started and never what became of it.
- The command line is a contract with another program: the global `--profile`/`-p` and the `chat -q` subcommand option are the Hermes CLI's, and a `-q` invocation on non-TTY stdio answers and exits, which is what a child process gets (Hermes Agent, *CLI Commands Reference*, read 2026-10-12). A Hermes upgrade can change it, and nothing in this repository's tests covers it; it is checked by hand on the host.
- A wake that arrives while an agent's run is in flight does not queue. The rule is one run per agent, and nothing waits its turn; what the service does instead is look again when that run ends ([008](008-the-service-acts-on-the-board.md)), so the card the dropped wake concerned is not left behind.
