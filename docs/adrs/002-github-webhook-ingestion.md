# 002 - GitHub Webhook Ingestion

## Context

GitHub posts to the service when something happens in the `aivara-se` organisation, and the receiver turns a delivery into one decision: which agents to wake, or none. The flow it carries is pull request reviews ([PRODUCT](../PRODUCT.md)), and v1 wants the smallest receiver that serves it. What a crash or a restart makes of a delivery is deliberately out of scope here.

The webhook is scoped to the organisation, so one route receives every repository the organisation owns. That scope is the set: a delivery arriving at the endpoint belongs to `aivara-se` by construction, and the receiver does not re-check the repository.

## Decision

- **One route, one secret.** A single POST path serves the organisation webhook, verified against one HMAC secret. There is no per-repository route and no allowlist of repositories.
- **Verify before parsing.** The signature is `X-Hub-Signature-256` — an HMAC-SHA256 over the raw request bytes, hex-encoded and prefixed with `sha256=` — compared in constant time. Nothing is unmarshalled until it verifies, and the body is capped at 1 MiB.
- **The webhook is configured content type `json`.** GitHub's other option, `form`, sends the body as `payload=<urlencoded JSON>`; the signature still verifies over the raw bytes, but the JSON decode then fails and the delivery is refused for a reason that has nothing to do with it.
- **One event carries the review request: `pull_request` with action `review_requested`.** It covers both ways a pull request asks for a review — a reviewer added, and a review requested again. A `ping` is accepted and ignored; so is every other event, action and repository. The second delivery v1.1 acts on, a submitted review that requests changes, is [`006`](006-the-review-loop-and-its-cap.md)'s.
- **The wake set comes from the payload.** The requested reviewer's login is matched against the service's configured agents, each a GitHub login beside the local profile it runs as; the match names the profile to wake. A requested team is ignored, and the delivery's own actor is never woken.
- **The decision costs no API call.** The payload and the configuration answer the whole of which agents to wake; the one exception is the `blocked` label the review loop reads and writes ([`006`](006-the-review-loop-and-its-cap.md)).
- **A delivery seen before wakes nobody.** The dedup key is `X-GitHub-Delivery`, held in memory for one hour (provisional; the first live week freezes the number).
- **The reason is identifiers.** The wake carries the repository and the pull request number in the service's own words, never a title, a body, a branch or any other text a third party wrote.
- **The answer set is small.** `401` for a missing or non-verifying signature, `400` for a body that is not JSON, `202` for every delivery that verifies — whether it acted or was ignored. The audit line, not the status, tells the two apart, and no body is ever logged.

## Consequences

- The endpoint can be exposed with a clear conscience: an unsigned request costs a header check and nothing else.
- One secret is one thing to rotate; a leaked secret is every delivery's, which is the price of there being one route.
- The dedup cache is the whole of the state ingestion keeps for a decision, and it dies with the process: a restart inside the hour can wake one agent twice. That is accepted, because a wake is a reason the agent re-reads, so a repeat is harmless — and because restart handling is out of scope for v1.
- A login the configuration does not carry wakes nobody and is not an error: the organisation can add reviewers who are not agents, and they are simply ignored.
- The service holds one GitHub credential, and only for the label: the decision itself costs no API call, so an outage of the API cannot withhold a wake the payload asked for ([`006`](006-the-review-loop-and-its-cap.md)).
