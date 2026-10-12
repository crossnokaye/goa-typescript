## examples/toy

This is a tiny Goa design, with a server, used to **doc-test** the goa-typescript plugin.

It is a Go module of its own (`github.com/xeger/goa-typescript/examples/toy`, with a `replace` pointing at the repository root): `cmd/toy` imports generated packages that are not committed, and keeping them out of the root module keeps `go vet ./...` green on a fresh checkout.

### What it proves

- `go run goa.design/goa/v3/cmd/goa@v3.34.0 gen ...` generates normal Goa `gen/` output **plus** `gen/ts`, a TypeScript package named `toy-client` (because the design blank-imports the plugin).
- The generated client **compiles** under TypeScript 5.6.2 with `strict` and `exactOptionalPropertyTypes`.
- Against the real goa server in `cmd/toy`, the vitest suite in `ts/` shows that:
  - every type feature round-trips: dates, bytes, enums, defaults, unions, recursion, views, a renamed JSON key and the `null` goa sends for it;
  - Server-Sent Events work as designed: whole events, events mapped onto `data`/`id`/`event`/`retry`, resuming with `Last-Event-ID`, `POST` bodies, multi-line text, primitives, aborting, breaking out;
  - goa errors arrive typed by name, including the ones goa raises itself;
  - credentials are hoisted out of the payload (JWT header, API key in the query) and `NoSecurity()` methods send none;
  - two services can reuse a method name (`toy.streamThingsSse`, `echo.streamThingsSse`) and share a type.

### How to run it locally

From this directory:

```bash
# Use a Goa CLI version compatible with the repo (this repo pins goa to v3.34.0).
go run goa.design/goa/v3/cmd/goa@v3.34.0 gen github.com/xeger/goa-typescript/examples/toy/design -o .
pnpm install --no-frozen-lockfile
pnpm type-check

go run ./cmd/toy &   # prints: listening on http://127.0.0.1:PORT
TOY_BASE_URL=http://127.0.0.1:PORT TOY_TOKEN=toy-token pnpm test
```

Or let the Go test do all of it, from the repository root:

```bash
go test ./plugin/typescript
```
