# dispatcher

The service that reads GitHub's deliveries for the `aivara-se` organisation and wakes the agents the board's state calls for, with the reason. It does not do the work: a wake says *why now*, and the agent re-reads the board and the pull request.

## Run it locally

From the repository root:

```sh
go build ./cmd/dispatcher     # ./dispatcher, in the repository root
./dispatcher --config <path>
```

Go 1.26 or newer, standard library only — the module carries no requires ([`001`](docs/adrs/001-go-and-the-standard-library.md)).

## Checks

```sh
gofmt -l .        # empty output is the pass
go vet ./...
go test ./...
```

These three are the gate; what they must cover is `SYSTEMS.md` (*Checks*). With no packages in the module yet, `go vet ./...` and `go test ./...` report `no packages to vet` and `no packages to test`.

## Documentation

- [`docs/PRODUCT.md`](docs/PRODUCT.md) — what the service is for, who it is for, and how it is judged
- [`docs/SYSTEMS.md`](docs/SYSTEMS.md) — the architecture: where it runs, the request path, and the parts
- [`docs/adrs/`](docs/adrs/) — one decision per file; merged into `main` means accepted
