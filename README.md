# Dispatch

Dispatch is the process engine for the `aivara-se` agent fleet. It reads a GitHub delivery, decides the next task in a job, prompts the Hermes profile that owns it, and proves the step on GitHub before it advances. Agents keep their own credentials; the engine reads, and writes only the labels a flow names. [`PRODUCT.md`](PRODUCT.md) says what it does; [`SYSTEMS.md`](SYSTEMS.md) says how it is shaped.

## Run it locally

```sh
go build ./cmd/dispatch
DISPATCH_WEBHOOK_SECRET=... ./dispatch --config config
```

`--db` names the record (default `~/.dispatch/dispatch.db`); `--socket` names the API socket (default `~/.dispatch/dispatch.sock`). The secret is read from the environment variable `secret_name` names; the App key is the file `key_path` names.

## Use it

The server serves the record on a Unix socket; the CLI and the web client read it. See [`config/`](config/), an example, and the [decisions](docs/adrs/).

## Check it

```sh
gofmt -l .        # empty
go vet ./...
go build ./...
TMPDIR=$HOME/tmp go test ./...

bun install --frozen-lockfile
bun x tsc -p tsconfig.json --noEmit
bun test

# From a checkout of aivara-se/.github:
bun run scripts/validate-docs.ts <path to this repository> --require
```

CI runs the same checker in the `docs` job.
