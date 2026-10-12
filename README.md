## goa-typescript

`github.com/xeger/goa-typescript` provides:

- **`plugin/typescript/`**: a Goa v3 codegen plugin that generates a TypeScript client into `gen/ts`, straight from the design expressions (no OpenAPI step).
- **`runtime/`**: the small TypeScript runtime that the plugin copies into every generated client (fetch client, typed goa errors, Zod codecs, Server-Sent Events).

The generated client is functional (`fn(client, payload, options?)`), returns `Result` values instead of throwing, types goa errors by name, and pairs every design type with a [Zod 4](https://zod.dev) schema of the same name.

**Status:** Server-Sent Events endpoints are generated today. Types and schemas are generated for every HTTP endpoint; functions for REST and WebSocket endpoints are on the [roadmap](#roadmap).

### Try it on a service (minimal)

`goa-typescript` targets Goa `v3.34.x` (Go 1.26+). The generated code needs TypeScript 5.6+ and `zod` `^4.1.0`.

The Goa plugin model is: your plugin must be linked into the generator binary that runs during `goa gen` ([plugin guide](https://pkg.go.dev/goa.design/plugins/v3)).

1. Add a blank import in your design module (any file in the design package):

```go
import _ "github.com/xeger/goa-typescript/plugin/typescript"
```

2. Run generation:

```bash
# IMPORTANT: your `goa` CLI must be compatible with the `goa.design/goa/v3` module
# version used by your repo. If you see a compile error like:
#   "not enough arguments in call to generator.Generate"
# it means your installed `goa` binary is older/newer than the module.
#
# This repo pins goa to v3.34.0, so this is a safe invocation:
go run goa.design/goa/v3/cmd/goa@v3.34.0 gen <design-import-path> -o .
# or, if your module declares goa as a tool:
go tool goa gen <design-import-path> -o .
```

3. Install the generated package's dependencies. `goa gen` deletes everything under `gen/`, `gen/ts/node_modules` included, so do this after every generation:

```bash
pnpm install
```

Design metadata the plugin reads:

| Meta | Where | Effect |
|---|---|---|
| `Meta("typescript:package", "@repo/api")` | `API` | npm package name (default `<api-name>-client`) |
| `Meta("typescript:generate", "false")` | `Service`, `Method` | leaves the service or method out |
| `Meta("typescript:name", "Thing")` | `Type`, `ResultType` | TypeScript name of the type |

### Generated layout

```
gen/ts/
├── package.json            name from Meta("typescript:package"); "type": "module"; peer dependency on zod
├── tsconfig.json           strict, bundler module resolution; used by the package's own `type-check` script
└── src/
    ├── index.ts            re-exports the runtime, the types, and one namespace per service
    ├── types.ts            every type the design declares, each with its Zod schema
    ├── runtime/*.ts        the runtime, copied verbatim
    └── <service>/
        ├── index.ts        barrel for the service
        └── <method>.ts     one function per method: its request type, error table, error union
```

The package is TypeScript source, not a build: `package.json` exports point at `.ts` files, and relative imports use the `.js` suffix that bundler module resolution maps back to them.

```ts
import { createClient, toy, unwrapAsync, type Thing } from "toy-client";
import { streamThingsSse } from "toy-client/toy"; // same function, importable on its own
import { Thing as ThingSchema } from "toy-client/types";
```

### Client configuration

```ts
import { createClient } from "toy-client";

const client = createClient({
  baseURL: "https://api.example.com", // a path prefix is kept; a relative URL works in a browser
  auth: () => getAccessToken(),       // string or function, sync or async, asked on every call
  timeoutMs: 10_000,                  // bounds the wait for response headers; streams are not cut off
  headers: { "X-Client": "web" },
  middleware: [
    async (req, next) => {
      const res = await next(req);
      console.debug(req.method, req.url, res.status);
      return res; // read res.clone(), never res itself: the body belongs to the caller
    },
  ],
});
```

**Credentials.** The attributes a design marks with `Token`, `AccessToken`, `APIKey`, `Username` and `Password` are left out of the request types. The client fills them in from `auth`, in the header or query parameter the design maps them to:

- a bare provider is used for every security scheme;
- `auth: { jwt: getToken, api_key: "k" }` selects a provider by scheme name;
- a provider that returns `undefined` sends the request without that credential;
- a bearer token gets the `Bearer ` prefix unless it already contains a space; basic credentials are `username:password`.

Methods declared with `NoSecurity()` never receive a credential.

**Per-call options** are the last argument of every function: `signal`, `headers`, `timeoutMs`, `baseURL`, and any other `RequestInit` field (`cache`, `credentials`, ...).

### Streaming methods (SSE)

A method with `StreamingResult` and `ServerSentEvents()` becomes a function that resolves to an async-iterable `EventStream`:

```ts
import { toy } from "toy-client";

const result = await toy.streamThingsSse(client, { id: "abc" }, { signal });
if (!result.ok) {
  return handle(result.error);
}
for await (const event of result.value) {
  render(event.thing); // typed, validated, dates already Date objects
}
// The loop ends when the server closes the stream.
```

- The stream yields the method's whole event type. Attributes that the design maps with `SSEEventID`, `SSEEventType` and `SSEEventRetry` are filled from the frame.
- `stream.lastEventId` and `stream.retry` expose the most recent `id:` and `retry:` values.
- Leaving the loop (`break`, `return`, an exception) or calling `stream.close()` cancels the response.
- An event that does not match its schema throws `ResponseValidationError` from the loop and closes the stream. Aborting `signal` throws `AbortError`.
- Errors the server reports before the first event are ordinary [typed errors](#errors) in `result.error`.

**There is no automatic reconnection.** The stream is a plain `fetch` response (so it can send headers and `POST` bodies, unlike `EventSource`), and when to reconnect is the application's call. Resume by passing the last event ID back; the client sends it as `Last-Event-ID`:

```ts
let lastEventId: string | undefined;
while (!signal.aborted) {
  const result = await toy.streamWrappedSse(client, { id }, { signal, lastEventId });
  if (result.ok) {
    try {
      for await (const { data } of result.value) {
        render(data);
        lastEventId = result.value.lastEventId;
      }
    } catch (e) {
      if (signal.aborted) break;
      report(e);
    }
  } else if (result.error.kind === "goa" && !result.error.temporary) {
    throw result.error; // not worth retrying
  }
  await sleep(backoff());
}
```

When the design names the payload attribute that carries the ID (`SSERequestID("last_event_id")`), that attribute is left out of the request type: `options.lastEventId` supplies it.

### Models and schemas

Every type the design declares with `Type` or `ResultType` gets one TypeScript type and one Zod schema **with the same name**, the usual Zod idiom:

```ts
import { Thing, decode, decodeJSON, encode } from "toy-client";

const thing: Thing = { id, kind: "widget", enabled: true }; // the type
const parsed = decode(Thing, json);                          // the schema: wire -> local, Result<Thing, ...>
const fromText = decodeJSON(Thing, text);
const wire = encode(Thing, thing);                           // local -> wire, validated
```

Inline types (a `Payload(func() { ... })`, a nested object, a primitive union branch) are rendered in place; the plugin does not invent names.

| goa | TypeScript | Notes |
|---|---|---|
| `Boolean`, `String`, `Float32`, `Float64` | `boolean`, `string`, `number` | |
| `Int`, `Int32`, `Int64`, `UInt`, `UInt32`, `UInt64` | `number` | checked to be a safe integer: a value past 2^53 fails validation rather than losing precision |
| `Format(FormatDateTime)` | `Date` | RFC 3339 with any offset on the wire |
| `Format(FormatDate)` | `string` | `YYYY-MM-DD`; never a `Date`, which would drag a time zone in |
| `Format(FormatUUID)`, email, hostname, ipv4, ipv6, ip, cidr | `string` | validated |
| `Format` uri, mac, regexp, json, rfc1123 | `string` | not validated |
| `Bytes` | `Uint8Array` | base64 on the wire; a plain `string` when sent as a query parameter or header |
| `Any` | `unknown` | |
| `ArrayOf(T)`, `MapOf(K, T)` | `T[]`, `Record<string, T>` | |
| `Enum(...)` | union of literals | closed: an unknown value fails validation |
| `OneOf(...)` | `{ type: "a"; value: A } \| { type: "b"; value: B }` | the envelope goa puts on the wire; key names follow `oneof:type:field` / `oneof:value:field` |
| `Pattern`, `Minimum`, `MaxLength`, ... | | enforced by the schema, on requests and on responses |

Things worth knowing:

- **Property names are the wire names.** No case conversion: a `created_at` attribute is `created_at` in TypeScript. `Meta("struct:tag:json", "legacyName")` renames the property, as it renames the JSON key.
- **Optional means absent.** goa omits unset optional attributes, so types use `name?: T`, never `null`. The one exception goa has is handled for you: a `struct:tag:json` override without `,omitempty` makes goa send `null`, which decodes to absent.
- **Defaults.** Inside a declared type, an attribute with a `Default` is not optional: decoding fills it in. At the top level of a request it is optional, and omitted from the request, because the server applies the default.
- **Views.** A result type is one TypeScript type; attributes missing from any of its views are optional.
- **Recursive types** work (`parent?: Thing`).
- **Unknown attributes are dropped** when decoding, so a server can add attributes without breaking deployed clients.
- **`Pattern`s that only RE2 understands** (`(?i)`, `(?P<name>`, `\z`, POSIX classes) are not enforced; the schema carries a comment saying so.

### Errors

Functions never throw. They return `Result<T, E>`:

```ts
type Result<T, E> = { ok: true; value: T } | { ok: false; error: E };
```

`unwrap(result)` and `unwrapAsync(promise)` turn a failure back into an exception when that is more convenient (inside a TanStack Query `queryFn`, for instance).

Every function exports its own error union. Every member has a string `kind`:

| `kind` | Class | Meaning |
|---|---|---|
| `"goa"` | `GoaError<Name, Detail>` | the service returned an error; `name` is the goa error name |
| `"network"`, `"abort"`, `"timeout"` | `NetworkError`, `AbortError`, `TimeoutError` | no response |
| `"request_validation"` | `RequestValidationError` | the request does not satisfy the design; nothing was sent |
| `"response_validation"` | `ResponseValidationError` | the response does not satisfy the design |
| `"unexpected_response"` | `UnexpectedResponseError` | a status or body the design does not describe (a proxy's 502 page, say) |

```ts
const result = await toy.streamThingsSse(client, { id });
if (!result.ok) {
  const e = result.error; // toy.StreamThingsSseError
  if (e.kind === "goa") {
    switch (e.name) {
      case "not_found": // names declared in the design autocomplete
        return notFound();
      case "unauthorized":
        return signIn();
    }
    if (e.fault) reportServerBug(e.id, e.message);
  }
  toast(errorMessage(e));
}
```

`GoaError` carries goa's `id`, `message`, `temporary`, `timeout` and `fault`, plus the HTTP `status`, `headers` and parsed `body`. Errors that goa raises on its own, such as request validation failures, arrive as `GoaError` too, under names the union cannot list (`GoaError<OtherErrorName>`). `errorMessage(e)` renders any of these for display.

### Consuming from a pnpm workspace

The generated package is meant to be a workspace member consumed as source:

1. Name it: `Meta("typescript:package", "@repo/api")` on the `API`.
2. Add `gen/ts` to `pnpm-workspace.yaml`.
3. Depend on it from the UI package: `"@repo/api": "workspace:*"`, together with `"zod": "^4.1.0"`. (Zod 4 still ships `zod/v3`, so code written against Zod 3 keeps working during a migration.)
4. Regenerate, then `pnpm install`: generation wipes `gen/ts/node_modules`.
5. With Vite, add the package to `optimizeDeps.exclude`; exclude `gen/ts/**` from linting.

### Roadmap

- **REST**: functions for unary endpoints, with multiple statuses, response headers, `Tag`-selected results and raw bodies.
- **WebSocket**: bidirectional and server-only streams over the native `WebSocket`.

Until then the service barrel lists the methods it skipped, and their types and schemas are generated all the same.
