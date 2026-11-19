# dispatch

The CLI reads the record from the server's Unix socket. It holds no state and
writes nothing.

```sh
dispatch flows
dispatch jobs [--flow F] [--state S]
dispatch show <flow>/<owner>/<repo>/<key> [--json]
dispatch watch
```

`--socket <path>` on every command; the default is
`~/.dispatch/dispatch.sock`.

`flows` prints each flow and its job count. `jobs` prints one line per job:
key, state, verdict and age. `show` prints the job, its tasks and each action's
output; `--json` prints the API object. `watch` follows the stream and prints
one line per change.

Exit codes: 0 on success, 1 when the flow or job is not found, 2 when the
socket is missing or the reply is bad.
