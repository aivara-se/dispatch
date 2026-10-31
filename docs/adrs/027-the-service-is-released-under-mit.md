# 027 - The Service Is Released Under MIT

## Context

The repository is public and every version ships as a GitHub release ([`025`](025-a-release-is-a-tag-of-version.md)); the module at `github.com/aivara-se/dispatch` is read through the Go module proxy. No file stated a licence, so the default held: the copyright holder reserves every right, whatever a public repository suggests. The operator chose MIT for the organisation's projects.

## Decision

- **MIT, in `LICENSE` at the repository root**, with the copyright line `Copyright (c) 2026 AIvara`. It covers the code and the documents this repository authors.
- **Every tag cut from this tree carries it**, and nothing is added to the module to say so: the licence is a file beside the code.
- **No dependency changes**, and a third-party file keeps the licence it arrived with.

## Consequences

- Anyone may use, modify and redistribute the service, commercially included, if the copyright notice and the permission text travel with it. MIT grants no patent licence.
- **It cannot be withdrawn from what is already out.** Tags released before this commit hold no licence file and are left as they are, so the grant runs from this commit onward.
- **The compiled binary embeds no notice.** A build from this tree is distributed under the licence in the source it came from, and the release page names that source; putting the text inside the binary is a packaging decision this record does not make.
