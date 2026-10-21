## examples/toy

This is a tiny Goa design, with a server, used to **doc-test** the goa-typescript plugin.

It is a Go module of its own (`github.com/crossnokaye/goa-typescript/examples/toy`, with a `replace` pointing at the repository root): `cmd/toy` imports generated packages that are not committed, and keeping them out of the root module keeps `go vet ./...` green on a fresh checkout.

### What it proves

- `go run goa.design/goa/v3/cmd/goa@v3.34.0 gen ...` generates normal Goa `gen/` output **plus** `gen/ts`, a TypeScript package named `toy-client` (because the design blank-imports the plugin).
- The generated client **compiles** under TypeScript 5.6.2 with `strict` and `exactOptionalPropertyTypes`.
- Against the real goa server in `cmd/toy`, the vitest suite in `ts/` shows that:
  - every type feature round-trips: dates, bytes, enums, defaults, unions, recursion, views, a renamed JSON key and the `null` goa sends for it; an inline enum named with `Meta("typescript:name")` is a type and schema of its own, and the request-side `ThingRequest` lets a request leave a nested defaulted attribute out for the server to fill in;
  - REST methods work as designed (`ts/rest.test.ts`): path, array, boolean and date-time parameters and a request header; a response header mapped onto a result attribute; views and `CollectionOf`; a `201`; a tagged `201` beside a `200`, and a tagged `200` whose body is one attribute beside a `204`; no content; a raw download left unread on the response and a raw upload of a `Blob`; primitive and `Bytes` results; `result.response` on success and on failure;
  - a method that offers a plain result and a stream gets two functions, and the plain one sends `Accept: application/json` (the server refuses anything else);
  - custom error types are decoded into `detail`: a named one with an attribute from a header, an unnamed one, and a plain string;
  - Server-Sent Events work as designed: whole events, events mapped onto `data`/`id`/`event`/`retry`, resuming with `Last-Event-ID`, `POST` bodies, multi-line text, primitives, aborting, breaking out;
  - WebSocket methods work as designed (`ts/websocket.test.ts`) over Node's global `WebSocket`: a server stream of a named type opens, yields and ends when goa closes with code 1000; a bidirectional stream answers what it is sent and ends after `end()` sends goa's `null`; a client stream yields its one result after `end()` although goa drops the connection without a close frame; an invalid message is returned by `send` and never sent; aborting closes the socket with 1000 and throws `AbortError`, and breaking out of the loop closes it too; a close code other than 1000 throws a `NetworkError` carrying it; an API key travels in the query string, while an error returned before the upgrade, or a JWT the browser API cannot put in a header, is a `NetworkError`; a `webSocket` factory that sends `init.headers` authenticates with that JWT;
  - the TanStack Query layer (`Meta("typescript:query", "tanstack")`) works against a real `QueryClient` from `@tanstack/query-core`, without React (`ts/query.test.ts`): `fetchQuery` of a read returns the decoded value under the documented key, with no credential in it, and a read without a result (`ping`) resolves to `null`; a dependent query with an `undefined` payload is disabled and sends nothing; an invalid payload never touches the list's cache entry; a `POST` overridden to a query is keyed by its body in wire form; a `HEAD` and the `GET` on its route, and the two functions of a mixed method, keep apart in the cache, and writing through `streamOrListStreamKey()` leaves the plain entry alone; Server-Sent Events and WebSocket streams go through the real `experimental_streamedQuery`, accumulate or fold with a reducer, and close their connection when the query is cancelled; mutations invalidate the nearest key a read lives under (a raw upload under `/content` invalidates its thing), or the key of the read they name with `typescript:invalidates`, filled in from their path parameters (`PUT /labels/{id}` invalidates that thing alone), and refetch an observed one, but never abort an open stream; the returned objects are assignable to query-core's `QueryObserverOptions` and `MutationOptions`;
  - goa errors arrive typed by name, including the ones goa raises itself;
  - credentials are hoisted out of the payload (JWT header, API key in the query) and `NoSecurity()` methods send none;
  - two services can reuse a method name (`toy.streamThingsSse`, `echo.streamThingsSse`) and share a type.

### How to run it locally

From this directory:

```bash
# Use a Goa CLI version compatible with the repo (this repo pins goa to v3.34.0).
go run goa.design/goa/v3/cmd/goa@v3.34.0 gen github.com/crossnokaye/goa-typescript/examples/toy/design -o .
pnpm install --no-frozen-lockfile
pnpm type-check

go run ./cmd/toy &   # prints: listening on http://127.0.0.1:PORT
TOY_BASE_URL=http://127.0.0.1:PORT TOY_TOKEN=toy-token pnpm test
```

Or let the Go test do all of it, from the repository root:

```bash
go test ./plugin/typescript
```
