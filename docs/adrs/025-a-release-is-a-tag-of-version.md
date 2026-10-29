# 025 - A Release Is a Tag of `VERSION`

## Context

The service has versions — the product history runs to v1.16 — but no file states the one the tree is, and nothing builds it anywhere but the host. The organisation's release standard reads a version file on a push to the default branch, tags that commit and attaches what it built, and this repository now follows it.

## Decision

- **`VERSION` at the root is the service's version**, `major.minor.patch`, raised in the change that earns it. The service does not read it.
- **A push to `main` is the release.** The publish workflow cross-compiles `./cmd/dispatch` for Linux amd64 and arm64 from the pushed commit, tags it `v<version>` when every build succeeds, and creates the GitHub release with `dispatch-<version>-linux-<arch>` attached. A version already tagged and released is not built again.
- **The tag publishes the module too.** `github.com/aivara-se/dispatch` is public, so the module proxy reads the tag; no job pushes to a registry.
- The first release is v1.16.0, the version the product history has reached.

## Consequences

- The current version has one home and the release page agrees with it; a version that is tagged but has no release is finished by the next push, not by a new commit.
- The binary carries no version of its own: `VERSION` is read by the workflow, so a binary is identified by the release it came from. Embedding the number is a change to the command, to be decided when something needs it.
- The build is the gate's own `go build`, cross-compiled, so a release adds no check the gate did not run; the second architecture is compiled, not tested.
- The host still builds from the tree and its unit file is unchanged. Running the released binary instead is a deployment decision this record does not make.
