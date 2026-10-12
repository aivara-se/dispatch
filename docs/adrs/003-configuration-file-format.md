# 003 - Configuration File Format

## Context

The service is configured by one document that is committed to this repository and read by humans in review: the listen address and the inbound path, the allowlist of repositories and events, the project, each agent's profile and login, and the *name* of each secret. It is behaviour, so its diff is reviewed like code, and it holds no secret value.

[001](001-go-and-the-standard-library.md) is the standard library only, and the standard library parses neither YAML nor TOML. The format is therefore a parser the module carries, and a trade-off rather than a detail: the loader cannot be written until it is settled.

## Decision

**JSON**, parsed with `encoding/json`. One format, one parser, and that parser is already in the binary — the receiver uses the same package on the request body, so the process holds one JSON decoder and not two.

Nothing else is added, for the configuration or for its example: no YAML, no TOML, no second format anywhere in the service. A future format is a future decision, recorded here before the code that needs it.

## Consequences

- Zero dependencies, which is `001`'s rule needing no exception.
- No comments and no trailing commas, so a hand-edited configuration is slightly less pleasant than YAML. Whoever edits it pays that, and the reviewer reads the diff either way.
- JSON is strict about what it accepts and silent about what it does not know: an unknown field parses cleanly and is dropped. Whether the loader refuses one is the loader's decision and its tests', not this record's.
- A secret stays a name either way: the format holds no value, so the lack of comments pushes nobody towards writing one down.

## Rejected

- **YAML.** One dependency and its supply chain for a format the standard library already covers, and it buys implicit typing — `no` is a boolean, a version is a number — anchors and duplicate-key ambiguity: three ways for a committed file to mean something other than what it reads as.
- **TOML.** Also one dependency, and its advantage over JSON is comments, which the reviewer of a diff supplies for free.
- **Configuration as Go source** — a package of literals, compiled in. Rejected: the configuration is behaviour that changes per deployment, so a change would become a rebuild and a redeploy rather than an edit and a restart.
- **Environment variables only.** Rejected: no diff to review, no one file that says what the service's whole configuration is, and the allowlist and the agent table scattered through a unit file.
- **Two formats**, YAML for the human and JSON for the service. Rejected: two parsers and two versions of one document, which is how the pair drifts.
