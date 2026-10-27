# 024 - A Superseded Delivery Names Its Replacement

## Context

`021` decides that a delivery landing on a live key is held, and that a later one for the same key replaces it: "A superseded delivery appends nothing further. Its line says a later delivery for the same key replaced it." The tree appends the held line when the delivery is held, and that line reads `held for <key>` and stops. The naming is on the later delivery's line instead:

```
line delivery=d-first    decision=started why=""
line delivery=d-held     decision=held    why="held for aivara-se/dispatch#105"
line delivery=d-replaces decision=held    why="held for aivara-se/dispatch#105, replacing delivery d-held"
line delivery=d-replaces decision=started why=""
```

`d-held` never runs, and its own line does not say so. Read alone it is the same line a delivery that is still waiting leaves behind, so the record cannot tell a wake that was dropped from one that is about to happen — the question `005` exists to answer.

`021`'s two clauses cannot both hold in one write. The later delivery does not exist when the earlier one's line is appended, and the line is never rewritten (`005`). Deferring the held line until its key resolves would name the replacement, and would cost the trace `021`'s own Consequences and PRODUCT's edge case lean on: a crash between a held delivery and its follow-up would then leave no line at all.

## Decision

- **The held line is appended when the delivery is held, unchanged.** It records `held` and the key it waits on, and it is the trace a crash leaves.
- **A superseded delivery appends one more line at the moment a later delivery for the same key replaces it**: decision `superseded`, its own delivery id and profile, and a `why` naming the key and the delivery that replaced it. This is where `021`'s "appends nothing further" stops; everything else in `021` stands.
- **The naming stays on the replacing delivery's line too**, which reads `replacing delivery <id>`: the two lines name each other, and each one explains its own arrival.
- **A superseded delivery starts nothing, spends no cap and is never retried.** It carries no `started` line, so `021`'s pairing — a held line paired with its follow-up's line by delivery id — is unchanged.
- **The outcome is a value in the `decision` field**, beside `started`, `held`, `ignored`, `duplicate` and `start_failed`, because the shape `005` fixes is one line per decision and not one per delivery.

## Consequences

- The record answers a superseded delivery's question: it was not started, and its line names the delivery that took its place, so a reader needs neither the later delivery's line nor an assumption about the order.
- A superseded delivery leaves two lines rather than one, and the reader takes the later as the outcome: `held` with nothing after it is a wake still waiting, `superseded` is a wake that will never run.
- The audit grows by one line per supersession, so a burst of n deliveries on one key adds n-1.
- A reader who stops at `021` still meets "appends nothing further". Immutability makes the correction a hop rather than an edit, which is the price of a record that keeps what it displaced.
