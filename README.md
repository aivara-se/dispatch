# dispatcher

The service that reads GitHub's deliveries for the `aivara-se` organisation and wakes the agents the board's state calls for, with the reason. It does not do the work: a wake says *why now*, and the agent re-reads the board and the pull request.

**The design is settled and the code has not started.**

## Run it locally

From the repository root, once `cmd/dispatcher` is written:

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

Until the packages exist, `go vet ./...` and `go test ./...` exit 1 with `no packages to vet` and `no packages to test`; `go build ./...` is the pass this module has today.

## Documentation

- [`docs/PRODUCT.md`](docs/PRODUCT.md) — what the service is for, who it is for, and how it is judged
- [`docs/SYSTEMS.md`](docs/SYSTEMS.md) — the architecture: where it runs, the request path, and the parts
- [`docs/adrs/`](docs/adrs/) — one decision per file; merged into `main` means accepted
