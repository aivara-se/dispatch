# 005 The engine uses one GitHub App

## Context

The engine reads pull requests and reviews, and writes labels. Agents write code, reviews and merges under their own identities. The engine needs a credential that cannot write the change.

## Decision

- One GitHub App, installed on the organisation's repositories. Its private key stays on the host; the engine mints installation tokens.
- Permissions:
  - Metadata: read.
  - Pull requests: read.
  - Issues: read and write, because a pull request's labels are issue labels.
- No contents write, no pull request write, no review write, no merge.
- The only write is the `blocked` label, through the label action.
- Webhooks arrive at one TLS endpoint with a secret, verified before parsing.
- The installation selects the repositories; the config's `repos` narrows the set the engine serves.

## Consequences

A missing permission is a bug, not an accident: the engine cannot change code even if asked. Contents and checks are not read in this version; a flow that needs them needs a new permission and a decision.
