# 023 - An Unsigned Delivery Costs Its Body Read

## Context

`002` says an unsigned request costs a header check and nothing else. The tree does not pay that price. The receiver reads the body through `io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))` before it verifies `X-Hub-Signature-256`, because the HMAC is over the raw bytes: the read cannot come after the check, or there would be nothing to check. The order is `002`'s decision and is unchanged here; its account of the price is what is wrong.

A probe posted 1 MiB unsigned to a running receiver: the delivery was answered `401` and allocated 2181 KiB. Nothing in the server bounds how many such reads are in flight — the timeouts are 5 s to read a header and 10 s to read the request, and no connection or concurrency limit is set.

## Decision

- **`002`'s clause "an unsigned request costs a header check and nothing else" stops.** Every other clause of `002` stands, the exposing of the endpoint included.
- **An unsigned request costs one body read**, of at most 1 MiB, held until the body arrives or the read timeout expires, and nothing more: no decode, no wake, no audit line.
- **How many such reads a stranger may hold at once is not decided here.** The service sets no bound; refusing a burst belongs in front of it or in the server, and that is a serving decision the operator makes.

## Consequences

- The record and the tree now say the same thing: read the body, verify it, then parse it. `002`'s "nothing is unmarshalled until it verifies" is still true, because reading is not parsing.
- The exposure is bounded per request and open in aggregate. N concurrent unsigned requests can hold N MiB at once, and the only limit today is the host's memory, so the endpoint is not safe to expose without something in front of it that refuses a burst.
- A reader who stops at `002` still meets the wrong sentence. Immutability makes the correction a hop rather than an edit, which is the price of a record that keeps what it displaced.
