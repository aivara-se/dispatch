# 004 - The Configuration

## Context

The service is configured by one document, committed to this repository and read by humans in review ([PRODUCT](../PRODUCT.md), *Configuration*). It is behaviour, so its diff is reviewed like code. It holds no secret value: the webhook secret is named and never written. What needs recording is what the document holds, and in what format.

## Decision

**One JSON file, standard library only.** `encoding/json`, the same parser the receiver already turns on a delivery's body, so the process holds one JSON decoder and not two. Nothing else is added, for the configuration or its example: no YAML, no TOML, no second format anywhere in the service.

The document holds, and holds nothing else:

- the **listen address** and the **path** the receiver answers on;
- the **name of the webhook secret**, which the process resolves from its environment at boot;
- the **absolute path of the `hermes` binary** a run launches ([003](003-starting-a-run.md)), which a systemd unit's minimal `PATH` cannot resolve;
- the **agents**: each one's GitHub login and the local profile that login runs as.

A name that resolves to nothing stops the process at boot rather than at the first delivery, and an unknown field is refused rather than dropped: a committed configuration with a typo in it should stop the service, not be read as a smaller one.

## Consequences

- Zero dependencies — `001`'s rule needs no exception here.
- No comments and no trailing commas, so a hand-edited configuration is slightly less pleasant than YAML. Whoever edits it pays that, and the reviewer reads the diff either way.
- The document is behaviour, so it lives in the tree and its changes are reviewed; but the whole of it is four things, and a fifth is a decision before it is a line.
