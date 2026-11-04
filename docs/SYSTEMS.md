# Dispatch: systems

## Architecture

One static Go process on the operator's host, as a systemd user unit on loopback behind the host's proxy, which terminates TLS. GitHub is its only public client.

`internal/receiver` verifies `X-Hub-Signature-256` over the raw body, with a 1 MiB bound, then decides, answers and holds the dedup cache. `internal/run` starts `hermes --profile <profile> chat -q "<reason>" --source tool -c "<title>" --create-if-missing` and reaps it. `internal/audit` appends one JSON Lines record per delivery, per boot and per run that ends non-zero, rotated by size. `internal/config` loads the JSON file and resolves the secret it names.

Every other piece of state is in memory: the dedup digests, each pull request's round and failure counts, each branch's failure count, and the one delivery held per live key. `cmd/dispatch` wires them, reads the one flag and shuts down cleanly. The process calls no GitHub API and holds no credential.

The argv is a contract with Hermes, checked by hand on the host. The decisions are ADRs in `docs/adrs/`.

### The reason

The reason is identifiers and the service's own words. The ten sentences are:

```
Dispatch: review requested on aivara-se/dispatch#105
Dispatch: changes requested on aivara-se/dispatch#105
Dispatch: approved but not armed on aivara-se/dispatch#105
Dispatch: issue assigned on aivara-se/dispatch#124
Dispatch: pull request assigned on aivara-se/dispatch#105
Dispatch: checks failed on aivara-se/dispatch#105
Dispatch: checks failed on aivara-se/dispatch@main
Dispatch: mentioned on aivara-se/dispatch#124
Dispatch: ready for review on aivara-se/dispatch#105
Dispatch: approval dismissed on aivara-se/dispatch#105
```

## Dependencies

Go 1.26 and its standard library only. The `hermes` binary, by absolute path. One organisation webhook of content type `json`, subscribed to `pull_request`, `pull_request_review`, `pull_request_review_comment`, `issues`, `issue_comment` and `workflow_run`. A reverse proxy to terminate TLS.

## Failure modes

- Wrong or missing signature: `401`, and nothing is parsed.
- Body that is not JSON, or past 1 MiB: `400`.
- Another event, action, review state or author association: `202`, nobody woken.
- A cap reached on a pull request or a branch: `202`, nobody woken.
- A run that cannot start: recorded, answered `202`, not retried.
- A run that never exits: its key stays held until the process restarts.
- A restart: the counts are lost, and one boot line marks it.
