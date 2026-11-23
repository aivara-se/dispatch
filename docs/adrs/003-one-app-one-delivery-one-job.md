# 003 - One App, one delivery, one job per agent

## Context

GitHub posts deliveries. The engine reads pull requests and reviews and writes labels, while agents write code, reviews and merges under their own identities. The engine needs a credential that cannot write the change, and a rule that turns a delivery into jobs without asking an agent.

A delivery can name several agents, and two deliveries can arrive while a job runs. One job per agent keeps the effects separate, and a key that carries the agent keeps two jobs from sharing an address.

## Decision

- One GitHub App, installed on the organisation's repositories: metadata and pull requests read, issues read and write for labels. No contents, pull request, review or merge write. Its key stays on the host; the engine mints installation tokens.
- One TLS endpoint answers against a secret, verified before parsing. The App subscribes to `pull_request` and `pull_request_review`; no flow names an event.
- The engine dedupes by delivery id, resolves the pull request, and starts one job per flow per pull request per agent whose login the delivery names. A login no agent holds starts nothing.
- A job's address is flow, owner, repo and key, and the key carries the agent. A flow whose start names a login carries `{agent}` in its key, and the loader refuses one that does not.
- A flow that already has a job on the pull request starts no second one. Jobs of different flows run in parallel.
- Every delivery the engine parses for a served repository leaves one row, `started`, `ended` or `ignored`, and an ignored one carries a reason from a closed set.
- The engine does not poll; a delivery is the only trigger. The record is local, so a restart resumes what is unfinished.

## Consequences

A new flow needs no new webhook event unless it starts on something other than a pull request. A delivery missed while the engine is down is not replayed, and the record keeps what arrived. The record grows with every delivery on a served pull request.
