# 023 - The Run's Child Environment

## Context

`003` starts each wake as a child of the service, and `exec.Command` with no `Env` hands that child the whole of the service's environment. The unit loads `DISPATCH_WEBHOOK_SECRET` through `EnvironmentFile` (`README.md`, *Run it on the host*), so every run — and everything an agent starts — carried the root of trust in its own environment: a run, or a prompt injection inside one, could sign deliveries that wake any configured agent ([#151](https://github.com/aivara-se/dispatch/issues/151)).

Taking the name out of the service's own environment once the configuration has read it (`os.Unsetenv`) closes the child's copy and costs a line, but it edits a process-wide thing from inside a loader, and it answers only for the one name the service knows: any other value the unit's environment carries still lands in every run.

## Decision

- **The configuration names the environment a child keeps.** `child_env` is a list of variable names in the one document `004` governs; it is required and it is not empty.
- **`config.Load` resolves the list at boot** into `NAME=value` pairs. A listed name the environment does not carry stops the process at boot rather than the first run, as an unresolved secret name already does, and the error names the variable and never a value.
- **A child is started with exactly those pairs** (`cmd.Env`). The service's own environment is neither passed on nor edited.
- **The secret's own name cannot be listed.** A document that lists it is refused at boot, because that list is the defect handed back.
- **Nothing else is added**: no deny-list, no filter on values, no default list in code. The names a Hermes run needs are the operator's declaration, and the document is where he makes it.

## What this overrides

`004`'s "the whole of it is four things, and a fifth is a decision before it is a line" is spent: the document holds five now, and this is the decision that clause asks for. `004`'s refusals stand unchanged — unknown field, a secret's value never in the file, a name that resolves to nothing stopping the boot.

`003`'s consequence "The command line is a contract this repository's tests cannot pin" is untouched. The environment is a second contract with the same program, and one a test *can* pin: it starts a child and reads what that child inherited.

## Consequences

- **The operator keeps a list per host.** A name Hermes, or a tool it starts, needs — `HOME` and `PATH` at least — is missing in the child if it is not listed, and the service cannot see the damage: `003` makes what became of a run the agent's own channel's business.
- **An existing host adds the field before the new binary boots**, because every field is required and an unknown or missing one refuses the document.
- **The child's environment is small and explicit**, so a value the agent's tools need from the unit's `EnvironmentFile` is there only because someone wrote it down.
- **The service keeps its own copy and its environment is unedited**, so anything that reads the process environment later sees what the unit started it with.
