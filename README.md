# Dispatch

A service that receives GitHub's webhook deliveries for the `aivara-se` organisation and wakes the agent a pull request asks to review, with the reason. It does not do the work: a wake says *why now*, and the agent re-reads the pull request and reviews it. v1 carries one flow, pull request reviews, and touches no board.

## Run it locally

```sh
go build ./cmd/dispatch     # ./dispatch, in the repository root
./dispatch --config dispatch.json
```

Go 1.26 or newer, standard library only — the module carries no requires ([`001`](docs/adrs/001-go-and-the-standard-library.md)). The one flag is `--config`; that file holds everything else, and the webhook secret is read from the environment variable it names ([`004`](docs/adrs/004-configuration.md)).

## Run it on the host

The service runs as a systemd user unit beside the Hermes home, binds loopback, and is reached through the host's reverse proxy, which terminates TLS. A run's child process needs the `hermes` binary by absolute path — a user unit's `PATH` is minimal and the bare name does not resolve there ([`003`](docs/adrs/003-starting-a-run.md)).

`~/.config/systemd/user/dispatch.service`:

```ini
[Unit]
Description=aivara dispatch
After=network-online.target

[Service]
Type=simple
WorkingDirectory=<dir holding dispatch.json>
ExecStart=<dir>/dispatch --config <dir>/dispatch.json
EnvironmentFile=%h/.config/dispatch.env
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
```

`~/.config/dispatch.env`, mode `600`, holds the webhook secret the configuration names and no other value:

```sh
DISPATCH_WEBHOOK_SECRET=<the secret GitHub is given>
```

The reverse proxy in front of it — the only networked hop — forwards the public path to the loopback address the configuration binds:

```
<hostname> {
	reverse_proxy 127.0.0.1:8645
}
```

Enable it:

```sh
loginctl enable-linger <user>       # or the unit dies with the last session
systemctl --user daemon-reload
systemctl --user enable --now dispatch
journalctl --user -u dispatch -f    # an audit line lands here when a delivery arrives
```

## Usage

The service answers one path, `POST /github` — the path the configuration names. Point the organisation webhook at it:

- **Payload URL** — `https://<hostname>/github`
- **Content type** — `application/json`. A `form` webhook sends `payload=<urlencoded>`, which the JSON decode refuses; the signature verifies either way, so the tell is the content type ([`002`](docs/adrs/002-github-webhook-ingestion.md)).
- **Secret** — the value in `DISPATCH_WEBHOOK_SECRET`
- **Events** — pull requests only

A verified `pull_request` delivery whose action is `review_requested` starts the named reviewer's run; every other delivery, and a redelivery, is answered and wakes nobody, which the audit record shows ([`002`](docs/adrs/002-github-webhook-ingestion.md), [`005`](docs/adrs/005-the-audit-record.md)).

## Checks

```sh
gofmt -l .        # empty output is the pass
go vet ./...
go test ./...
```

These three are the gate; CI runs them on every pull request and on `main`. What the tests must cover, and what no test can reach, is the architecture's business.

## Documentation

- [`docs/PRODUCT.md`](docs/PRODUCT.md) — what the service is for, who it is for, and how it is judged
- [`docs/adrs/`](docs/adrs/) — one decision per file; merged into `main` means accepted
