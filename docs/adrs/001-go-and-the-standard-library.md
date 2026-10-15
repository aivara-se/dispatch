# 001 - Go and the Standard Library

## Context

This service is a long-running network process that must be up when cron is not: it listens for GitHub's deliveries and wakes the agents the board's state calls for ([008](008-the-service-acts-on-the-board.md)). That is a different kind of program from the fleet's monitor scripts, which run, print and exit. Go is the language it is written in; what needs recording is why, and what the choice rules out.

## Decision

Go, standard library only. `net/http` serves the receiver and makes the outbound dispatch; `crypto/hmac` and `crypto/subtle` verify signatures; `encoding/json` parses what the verifier has already signed off. Nothing is added to the module's requires until a real need appears, and the bar for that need is written in the pull request that adds it.

The binary is `cmd/dispatcher`, with the receiver, the board, the router and the runner as separate packages under `internal/`, and the config and audit packages beside them (`SYSTEMS.md`, *Where it runs*). The language floor is Go 1.26, the version the organisation's other Go service builds against.

## Consequences

- One static binary to deploy: no runtime to install on the VPS, no dependency tree to update, no interpreter version to keep in step with the host.
- The process starts in milliseconds, which matters for a systemd unit that restarts on failure while GitHub is withholding deliveries.
- The whole surface is one route in and one request out, so there is nothing for a framework to do.
- What it rules out: sharing code with the fleet's TypeScript monitors. A routing or dedup rule written here cannot be reused by the poll, so the two mechanisms are written twice while both exist. That is accepted because the poll is meant to disappear, and because the shared fact — the board and the card — is data, not code.
- The cost is hand-rolling what a framework would supply: a mux, a middleware chain and a config loader. For one POST route that is a handful of lines, and it is code the reviewer can read in full.
