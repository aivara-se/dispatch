# 004 - The Configuration

## Context

The service is configured by one document, written on the host it runs on. It is behaviour: what it holds decides what the service does, so a change to its shape is a change to the product. It holds no secret value: the webhook secret is named and never written. What needs recording is what the document holds, and in what format.

## Decision

**One JSON file, standard library only.** `encoding/json`, the same parser the receiver already turns on a delivery's body, so the process holds one JSON decoder and not two. Nothing else is added, for the configuration or its example: no YAML, no TOML, no second format anywhere in the service.

The document holds, and holds nothing else — this is the whole of what may be configured:

- the **listen address** the receiver binds, and the **path** it answers on;
- the **name of the webhook secret**, which the process resolves from its environment at boot;
- the **absolute path of the `hermes` binary** a run launches ([003](003-starting-a-run.md)), which a systemd unit's minimal `PATH` cannot resolve;
- the **agents**: each one's GitHub login and the local profile that login runs as.

A name that resolves to nothing stops the process at boot rather than at the first delivery, and an unknown field is refused rather than dropped: a configuration with a typo in it should stop the service, not be read as a smaller one.

## Consequences

- Zero dependencies — `001`'s rule needs no exception here.
- No comments and no trailing commas, so a hand-edited configuration is slightly less pleasant than YAML. Whoever edits it pays that.
- The document is behaviour, so a change to its shape is a change to the product and not an edit; but the whole of it is four things, and a fifth is a decision before it is a line.
