# 001 - Go and the Standard Library

## Context

This service is a long-running network process: it listens for GitHub's deliveries and wakes the agent a delivery concerns. That is a different kind of program from a script that runs, prints and exits. Go is the language it is written in; what needs recording is why, and what the choice rules out.

## Decision

Go, using the standard library as much as possible. The standard library is preferred whenever it can do the job; a third-party dependency is added only when no standard-library equivalent exists. Nothing is added to the module's requires until a real need appears, and the bar for that need is written in the pull request that adds it.

## Consequences

- One static binary to deploy: no runtime to install on the VPS, no dependency tree to update, no interpreter version to keep in step with the host.
- The process starts in milliseconds, which matters for a systemd unit that restarts on failure while GitHub is withholding deliveries.
- The whole surface is one route in and one request out, so there is nothing for a framework to do.
