# 018 - The Service's Own Boot Reaches the Audit

## Context

`005` fixes the record as the delivery's, and `015` adds the run's end. Neither says the service was alive: a process that crashes at boot, or crash-loops under the unit that restarts it, leaves the file simply stopping. Every count's "dies with the process" consequence has no mark in the record where it died, so a reader cannot tell a quiet hour from a restart inside it. The unit's journal carries the same fact, but the operator reads this file.

## Decision

- **One line at boot**, appended after the configuration loads and the listener binds, before the first delivery can be served.
- **It carries the moment, the build's revision and whether its tree was modified** — read from the binary's own build info (`debug.ReadBuildInfo`), so no flag, no build variable and no pipeline change — **the listen address, and the number of configured agents**. Never a secret, and never the roster.
- **No heartbeat, no periodic line, no shutdown line.** A boot is the only event of the service's own that is recorded.
- **A boot line is not a delivery**: no delivery id, no event, no answer. It is appended in the order it happens, as every line is.

## What this overrides

[005](005-the-audit-record.md)'s "One JSON Lines line per delivery" and "Only a delivery that verified is logged". The record becomes one family with three kinds of line — a delivery, a run's end ([`015`](015-the-audit-records-a-runs-exit.md)), and a boot — so this is the last widening: a fourth kind would need its own reason to exist. `005`'s shape (JSON Lines, appended, never rewritten, size-rotated), its never-a-secret rule and its field list for a delivery line all stand.

## Consequences

- A restart becomes visible where the operator looks, and the line marks where the delivery cache and both counts reset.
- A binary built from a modified tree says so, which is the fact a deploy by binary swap needs; a build that carries no VCS metadata says unknown rather than guessing.
- A crash-loop writes one line per boot, so the file shows the cadence and not the fault: the cause remains the unit's journal.
- The record grows by one line per process, which is nothing beside the deliveries.
