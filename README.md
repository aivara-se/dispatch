# Dispatch

Dispatch is the process engine for the `aivara-se` agent fleet. It reads a GitHub delivery, decides the next task in a job, prompts the Hermes profile that owns it, and proves the step on GitHub before it advances.

## Setup

Go 1.26, Bun 1.4.4, Node, a Hermes binary, and a GitHub App with a webhook secret and a private key. `config/` is a complete example.

```sh
go build ./cmd/dispatch
bun install --frozen-lockfile
cd web && bun run build && cd ..
```

`config/dispatch.json` names the environment variable holding the secret (`secret_name`), the App's `key_path`, the served repositories, and the Hermes binary. `--db` names the record (default `~/.dispatch/dispatch.db`); `--socket` names the API socket (default `~/.dispatch/dispatch.sock`).

## Usage

Start the engine and point the TLS proxy at it:

```sh
DISPATCH_WEBHOOK_SECRET=... ./dispatch --config config
```

Serve the client from the socket:

```sh
cd web
DISPATCH_SOCKET=~/.dispatch/dispatch.sock node build
```

It answers on `:3000`; `PORT` changes it. Read the record with the CLI:

```sh
bun run dispatch flows
bun run dispatch jobs
bun run dispatch show review/aivara-se/dispatch/212-momo
```

Run the checks:

```sh
gofmt -l .        # empty
go vet ./...
go build ./...
TMPDIR=$HOME/tmp go test ./...
bun fmt --no-script --check .
bun x tsc -p tsconfig.json --noEmit
bun test cli
(cd web && bun run build && bun run check && bun test)
```

The behaviour and its limits are in [`docs/SPEC8N.md`](docs/SPEC8N.md); the interface tokens are in [`docs/DESIGN.md`](docs/DESIGN.md).

## License

MIT
