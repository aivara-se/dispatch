# Dispatch

A service that receives GitHub's webhook deliveries for the `aivara-se` organisation and wakes the agent an event concerns: the reviewer a pull request asks for, the author whose review requests changes or whose change is approved but not armed, the agent an assignment or a comment names, the pusher of a pull request or of the default branch whose checks failed, the author whose review was dismissed, or the reviewers of a draft marked ready. It does not do the work: a wake says *why now*, and the agent re-reads the pull request or the issue and acts. v1.16 carries six flows: the review and update loop, capped at ten rounds; an assignment, on an issue or on a pull request; a failing check on a pull request or on the default branch, five wakes each; a mention in a comment, in a review's body or in a review comment; a draft that becomes ready; and a dismissed review. One agent works one pull request or branch at a time: a delivery that lands behind its own run is held, and starts when that run exits ([`021`](docs/adrs/021-one-run-per-pull-request-per-agent.md)). A wake resumes the session kept for that object, so a later round builds on the earlier ones ([`022`](docs/adrs/022-a-wake-resumes-its-objects-session.md)). It touches no board.

## Configuration

The service reads one JSON file, the one `--config` names; write it beside the binary as `dispatch.json`. It is not committed — each host writes its own — and this is the document it expects:

```json
{
  "listen": "127.0.0.1:8645",
  "path": "/github",
  "secret_name": "DISPATCH_WEBHOOK_SECRET",
  "hermes": "/root/.local/bin/hermes",
  "agents": [
    { "login": "thani-sh-mama", "profile": "mama" },
    { "login": "thani-sh-meme", "profile": "meme" },
    { "login": "thani-sh-mimi", "profile": "mimi" },
    { "login": "thani-sh-momo", "profile": "momo" }
  ]
}
```

Every field is required and an unknown one is refused. `secret_name` names the environment variable the webhook secret is read from and the value is never in the file.

## Run it locally

```sh
go build ./cmd/dispatch     # ./dispatch, in the repository root
./dispatch --config dispatch.json
```

Go 1.26 or newer, standard library only — the module carries no requires. The one flag is `--config`; that file holds everything else, and the webhook secret is read from the environment variable it names.

## Run it on the host

The service runs as a systemd user unit beside the Hermes home, binds loopback, and is reached through the host's reverse proxy, which terminates TLS. A run's child process needs the `hermes` binary by absolute path — a user unit's `PATH` is minimal and the bare name does not resolve there.

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
journalctl --user -u dispatch -f    # the service's own log
```

## Usage

The service answers one path, `POST /github` — the path the configuration names. Point the organisation webhook at it:

- **Payload URL** — `https://<hostname>/github`
- **Content type** — `application/json`. A `form` webhook sends `payload=<urlencoded>`, which the JSON decode refuses; the signature verifies either way, so the tell is the content type.
- **Secret** — the value in `DISPATCH_WEBHOOK_SECRET`
- **Events** — pull requests, pull request reviews, pull request review comments, issues, issue comments and workflow runs

A verified `pull_request` delivery whose action is `review_requested` starts the named reviewer's run; one whose action is `ready_for_review` starts the run of every configured reviewer still requested on the pull request, a team not being resolved. A verified `pull_request_review` delivery whose review requests changes starts the pull request author's run, and so does one that approves a pull request that is open, not a draft and not armed; an approval that lands the change wakes nobody. A review on an open, non-draft, unmerged pull request that is dismissed starts the author's run. A pull request stops waking anyone once ten reviews have requested changes, and an approval or a dismissal spends no round. A verified `issues` delivery whose action is `assigned` starts the run of the login in its `assignee`; a `pull_request` delivery with the same action does the same for the pull request's `assignee`, and neither adds a round. A verified `issue_comment` delivery whose action is `created`, a submitted review and a created review comment each start the run of every configured login their text names exactly, when the author is the owner, a member or a collaborator. A verified `workflow_run` delivery whose run completes as `failure` on a pull request starts the run of the account that triggered it, at most five times per pull request; one that fails on the default branch with no pull request starts that account's run too, labelled with the branch, at most five times per repository and branch. Every other delivery, and a redelivery, is answered and wakes nobody, which the audit record shows.

## Checks

```sh
gofmt -l .        # empty output is the pass
go vet ./...
go test ./...
```

These three are the gate; CI runs them on every pull request and on `main`. What the tests must cover, and what no test can reach, is the architecture's business.

## Documentation

- [`docs/PRODUCT.md`](docs/PRODUCT.md) — what the service is for, who it is for, and how it is judged
- [`docs/SYSTEMS.md`](docs/SYSTEMS.md) — where it runs, how a delivery becomes a run, and what is checked
