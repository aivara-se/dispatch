# dispatch web

The SvelteKit client of the record. It reads the socket on the server; the
browser talks only to this app. There is no write path.

```sh
bun install
bun run build
DISPATCH_SOCKET=~/.dispatch/dispatch.sock node build
```

It serves on `:3000`; `PORT` changes it. `bun run dev` runs the development
server. The socket defaults to `~/.dispatch/dispatch.sock`; `DISPATCH_SOCKET`
names another.
