## goa-typescript

`github.com/xeger/goa-typescript` provides:

- **`plugin/typescript/`**: a Goa v3 codegen plugin that generates a TypeScript client into `gen/ts`, straight from the design expressions (no OpenAPI step).
- **`runtime/`**: the small TypeScript runtime that the plugin copies into every generated client (fetch client, response decoding, typed goa errors, Zod codecs, Server-Sent Events).

The generated client is functional (`fn(client, payload?, options?)`), returns `Result` values instead of throwing, types goa errors by name, and pairs every design type with a [Zod 4](https://zod.dev) schema of the same name.

**Status:** REST and Server-Sent Events endpoints are generated today. Types and schemas are generated for every HTTP endpoint; functions for WebSocket endpoints are on the [roadmap](#roadmap).

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
| `Meta("typescript:name", "ColorKey")` | an inline primitive, enum, array, map or union attribute | hoists the attribute as a named type and schema (see [Models and schemas](#models-and-schemas)) |

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
                            (two functions when the method offers both a plain result and a stream)
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

**One call shape.** Every function is `fn(client, payload?, options?)`, whatever the method declares ([ADR 0003](docs/adr/0003-one-call-shape.md)). The payload is always the second argument:

| Design | `payload` |
|---|---|
| a `Payload` with a required attribute (credentials do not count: they come from the client's `auth`) | required |
| a `Payload` whose attributes are all optional or have a `Default` | optional; `fn(client)` sends an empty payload |
| no `Payload`, or nothing left of it once credentials are stripped | `undefined`; `fn(client)` or, to pass options, `fn(client, undefined, { signal })` |

So a wrapper typed `(client, payload, options?) => Promise<CallResult<Res, EndpointError>>` accepts every function, and adding the first optional attribute to a method does not change how its callers look. Passing options to a payload-less method takes an `undefined` placeholder; passing them where the payload goes is a compile error rather than a silently misread request. The one exception is a [raw upload](#rest-methods) without a payload: `body` follows `payload`, and an optional parameter cannot precede a required one, so `payload` is a required `undefined` there.

### REST methods

A method without streaming becomes a function `fn(client, payload?, options?)` that resolves to the method's result:

```ts
import { toy } from "toy-client";

const result = await toy.getThing(client, { id: "abc" }, { signal });
if (!result.ok) {
  return handle(result.error); // see Errors
}
result.value;                        // ThingReply: decoded and validated, dates already Date objects
result.response.status;              // the fetch Response, for the status and the headers
result.response.headers.get("etag");

await toy.getThingViewed(client);                      // every attribute is optional: the payload may be omitted
await toy.listThings(client, undefined, { signal });   // no payload: undefined holds its place
```

**The value is the bare goa type.** There is no `{ data }` envelope: what the design calls the `Result` is what `result.value` is, and what `unwrapAsync` returns. Transport metadata rides beside it, on `result.response` ([ADR 0002](docs/adr/0002-response-metadata-beside-the-value.md)). On a failure `result.response` is set whenever a response arrived (a goa error, an unexpected response, a response that fails validation) and absent otherwise (network failure, abort, timeout, a request that fails validation).

| Design | Function |
|---|---|
| `Result(Thing)` | `value` is `Thing` |
| `Result(func() { ... })`, `Result(ArrayOf(String))`, `Result(Int)` | `value` has the type `<Method>Result`, declared in the method's file |
| `Result(CollectionOf(Thing))` | `value` is `Thing[]`; goa's `ThingCollection` name is not used |
| no `Result` | `value` is `void` |
| `Header("etag:ETag")` in a `Response` | the attribute is filled from the header: numbers and booleans are parsed, arrays are split on commas |
| `Cookie(...)` in a `Response` | the attribute is never set: script cannot read `Set-Cookie` |
| `Body("attr")` | the body is that attribute |
| several `Response`s | one value type; `result.response.status` says which response it was |
| `Tag("outcome", "created")` | the client sets `outcome` to `"created"` for that response, as goa's Go client does |

**Several statuses.** goa picks the response from the result's tag attribute and leaves that attribute out of the body. The client puts it back, so both views agree:

```ts
const result = await toy.upsertThing(client, { id, name });
if (result.ok) {
  result.response.status; // 200 or 201
  result.value.outcome;   // "updated" or "created"
}
```

A response without a body is decoded from its headers and tag alone. If the result type requires an attribute that such a response does not carry, the response cannot be valid: `goa gen` prints a warning, the function's documentation says so, and at run time that status fails with a `ResponseValidationError`. Make the attribute optional in the design, or give the response a body.

**Raw download** (`SkipResponseBodyEncodeDecode`). The value is built from the mapped headers and the body is left unread on the response. This is the one case where `result.response` still has its body:

```ts
const result = await toy.downloadThing(client, { id });
if (result.ok) {
  result.value.content_length;              // from the Content-Length header
  const blob = await result.response.blob(); // or arrayBuffer(), or result.response.body to stream it
}
```

**Raw upload** (`SkipRequestBodyEncodeDecode`). The function takes the body after the payload and hands it to `fetch` as is:

```ts
await toy.uploadThing(client, { id, content_type: file.type }, file);
```

When the method has no payload the function still takes one, typed `undefined`, so that `body` stays in its place: `upload(client, undefined, file)`. The same holds for a raw upload whose payload attributes are all optional: `payload` stays required there, because TypeScript does not allow an optional parameter before a required one.

A `Blob` (so a `File`), an `ArrayBuffer`, `FormData` and a string work everywhere. A `ReadableStream` is sent with `duplex: "half"`, which Safari and Firefox do not support. No `Content-Type` is set unless the design maps a payload attribute to that header; `fetch` derives one from a `Blob` or a string.

**A plain result and a stream** (`Result` and `StreamingResult` on one method). Two functions in one file: `streamOrList()` sends `Accept: application/json` and returns the plain result, `streamOrListStream()` is the [SSE function](#streaming-methods-sse). They share the request type and the error union.

**Not generated**, and listed with the reason at the bottom of the service barrel: `Redirect(...)` endpoints (`fetch` follows the redirect, so there is nothing to return), responses with a non-JSON `ContentType` that are not raw, response headers of type `Bytes` or `Any` (goa does not write them in a form the schema can read back), a raw request body on `GET` or `HEAD`, multipart and form-encoded requests, file servers.

Things worth knowing:

- **Cross-origin headers.** A browser shows script only a short list of response headers from another origin. Unless the UI and the service share an origin, the service must list every header a design maps to an attribute, plus `goa-error` and `goa-view`, in `Access-Control-Expose-Headers`. Without `goa-error` the name of an error is still read from the body of goa's default error type; a custom error type that shares its status code with another error cannot be told apart.
- **`void` and TanStack Query.** `unwrapAsync(toy.deleteThing(...))` resolves to `undefined`. That is right for `useMutation`. A `useQuery` `queryFn` must not resolve to `undefined`: return something else from it.
- **A 200 that is not JSON** (a proxy's login page, say) is an `UnexpectedResponseError` carrying the text, not a validation error.
- **`null` for an empty array or map.** A Go service encodes a nil slice or map result as `null`; the client reads it as empty.
- **Array headers** are split on commas, so an element that contains one does not survive. goa has the same limit on the way in.

### Streaming methods (SSE)

A method with `StreamingResult` and `ServerSentEvents()` becomes a function `fn(client, payload?, options?)`, shaped like a [REST function](#rest-methods), that resolves to an async-iterable `EventStream`:

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
- The result carries the fetch `Response` like any other function's: `result.response`, which is also `result.value.response` once the stream is open.

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

Inline types (a `Payload(func() { ... })`, a nested object, a primitive union branch) are rendered in place; the plugin does not invent names, but the design can give one. `Meta("typescript:name", "ColorKey")` on an inline primitive, enum, array, map or union attribute hoists it into `types.ts` as a named type and schema, carrying the attribute's validations, and the parent refers to it:

```go
Attribute("color_key", String, func() {
    Enum("blue", "green")
    Meta("typescript:name", "ColorKey")
})
```

```ts
export type ColorKey = "blue" | "green";
export const ColorKey = z.enum(["blue", "green"]) satisfies z.ZodType<ColorKey>;

export type Widget = { color_key: ColorKey; /* ... */ };
```

An inline object attribute cannot be named this way (declare a `Type` for it; `goa gen` says so), and a `Default` stays with the parent, which is what makes the attribute optional on the request side. Without a name, the usual TypeScript idioms reach an inline type: `Widget["color_key"]` for a required attribute, `NonNullable<Routine["presentation"]>` for an optional one.

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
- **Defaults.** Inside a declared type, an attribute with a `Default` is not optional: decoding fills it in. In a request it is optional, and omitted from the request, because the server applies the default. For that, every declared type with a defaulted attribute, and every type that refers to one, gets a request-side variant beside it, `ThingRequest`: the same type and schema with the defaulted attributes optional, referring to `OtherRequest` where `Thing` refers to `Other`. Request payloads use the variants, at any depth, so a nested defaulted attribute may be left out too. `ThingRequest` is also the type for a value built by hand, such as a fixture or a mock, which is why a type no request uses still gets one: `encode(ThingRequest, value)` sends it as is, and a decoded `Thing` is assignable to it. A type whose request side would not differ has no variant. Results and events keep the decoded type, defaults required. (Migrating from a client that typed defaulted attributes as optional: type the fixtures as `ThingRequest` rather than adding the defaults to each of them.) See [ADR 0004](docs/adr/0004-request-side-types.md).
- **Views.** A result type is one TypeScript type; attributes missing from any of its views are optional.
- **Recursive types** work (`parent?: Thing`).
- **Unknown attributes are dropped** when decoding, so a server can add attributes without breaking deployed clients.
- **`Pattern`s that only RE2 understands** (`(?i)`, `(?P<name>`, `\z`, POSIX classes) are not enforced; the schema carries a comment saying so.

### Errors

Functions never throw. They return `CallResult<T, E>`, which is a `Result<T, E>` with the fetch `Response` beside it:

```ts
type Result<T, E> = { ok: true; value: T } | { ok: false; error: E };
type CallResult<T, E> =
  | { ok: true; value: T; response: Response }
  | { ok: false; error: E; response?: Response };
```

`unwrap(result)` and `unwrapAsync(promise)` turn a failure back into an exception when that is more convenient (inside a TanStack Query `queryFn`, for instance). They return the bare value.

Every function exports its own error union. `EndpointError` is the type every union is assignable to, for code that handles errors from more than one endpoint:

```ts
import type { EndpointError } from "toy-client";

type QueryOptions<T> = UseQueryOptions<T, EndpointError>;
function toast(e: EndpointError) { /* ... */ }
```

Every member has a string `kind`:

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

`GoaError` carries goa's `id`, `message`, `temporary`, `timeout` and `fault`, plus the HTTP `status`, `headers`, parsed `body` and the `response`. Errors that goa raises on its own, such as request validation failures, arrive as `GoaError` too, under names the union cannot list (`GoaError<OtherErrorName>`). `errorMessage(e)` renders any of these for display.

**Custom error types.** An error declared with a type of its own (`Error("invalid", ValidationProblem)`, `Error("conflict", func() { ... })`, `Error("teapot", String)`) is decoded and validated into `detail`. An unnamed type is declared in the method's file as `<Method><Error>Detail`. Attributes that the error's `Response` maps to headers are merged in.

Use `isGoaError(e, name)` to reach the detail. Comparing `e.name` is not enough for the compiler, because an undeclared error may carry any name:

```ts
const result = await toy.createThing(client, { name });
if (!result.ok) {
  if (isGoaError(result.error, "invalid")) {
    result.error.detail.field;       // ValidationProblem
    result.error.detail.retry_after; // from the Retry-After header
  }
}
```

The narrowing needs the function's error union to pick from, so it works on `result.error`. In a `catch` block the value is `unknown`: `isGoaError(e, "invalid")` still checks the name, and `e.detail` is `unknown`.

### Consuming from a pnpm workspace

The generated package is meant to be a workspace member consumed as source:

1. Name it: `Meta("typescript:package", "@repo/api")` on the `API`.
2. Add `gen/ts` to `pnpm-workspace.yaml`.
3. Depend on it from the UI package: `"@repo/api": "workspace:*"`, together with `"zod": "^4.1.0"`. (Zod 4 still ships `zod/v3`, so code written against Zod 3 keeps working during a migration.)
4. Regenerate, then `pnpm install`: generation wipes `gen/ts/node_modules`.
5. With Vite, add the package to `optimizeDeps.exclude`; exclude `gen/ts/**` from linting.

### Roadmap

- **WebSocket**: bidirectional and server-only streams over the native `WebSocket`.
- **Multipart requests and file servers.**

Until then the service barrel lists the methods it skipped, and their types and schemas are generated all the same.
