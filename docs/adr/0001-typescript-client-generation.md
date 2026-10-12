# ADR 0001: Generate the TypeScript client from Goa expressions

- **Date:** 2026-10-11
- **Status:** Accepted
- **Supersedes:** none

## Context

CrossnoKaye's UI/BFF monorepos (hyperion, gaia) call their Goa BFFs through TypeScript SDKs that Speakeasy generates from Goa's OpenAPI output. Going through OpenAPI has three recurring costs:

1. **Goa errors arrive untyped.** Goa serves its errors as `application/vnd.goa.error`, which the SDK does not treat as JSON. Every non-2xx response becomes a generic error, and both UIs hand-parse its body for `{name, id, message}`.
2. **Streams are weak.** Server-Sent Events work but lose the `id`, `event` and `retry` fields and cannot resume with `Last-Event-ID`. WebSocket endpoints get REST functions that cannot work; the UIs use a WebSocket library and borrow only the SDK's parsers.
3. **Fidelity is lost.** Validations (`Minimum`, `Pattern`, `Format(FormatUUID)`...) are dropped; `Format(FormatDate)` becomes a `Date` that the UI has to rebuild at UTC midnight; property names are remapped from snake_case to camelCase by a transform.

The UIs use a small slice of what Speakeasy emits: standalone functions `(core, request, options?) -> Promise<Result<T, E>>`, the model types, the JSON parsers and `signal`. The SDK class, hooks, retries and pagination are never touched.

## Decision

`github.com/xeger/goa-typescript` is a Goa codegen plugin that reads the design expressions directly and emits a TypeScript client under `gen/ts`.

- **Goa-native, no OpenAPI.** The plugin reads `goa.design/goa/v3/expr` only. It does not use Goa's `codegen/service` or `http/codegen` data, whose constructors are no longer public, and it never mutates the design, which Goa now verifies after every plugin.
- **One Zod 4 codec per type.** Each declared type gets a TypeScript type and a Zod schema of the same name. The schema decodes (wire to local) and encodes (local to wire), so validation and conversions such as `Date` and `Uint8Array` are defined once. Generated schemas never use `.transform()`, which cannot run backwards.
- **The runtime is emitted into `gen/`.** Its sources live in `runtime/src`, are tested with vitest, embedded with `go:embed` and copied verbatim. The generated package's only dependency is `zod`.
- **No casing normalization.** A property is named after its JSON key: the attribute name, or the name set by `Meta("struct:tag:json")`.
- **Functional only.** `fn(client, payload, options?)` returning `Promise<Result<T, E>>`; one function per file so bundlers can drop what is unused. There is no SDK class and nothing throws except `unwrap`/`unwrapAsync`.
- **Typed Goa errors.** `GoaError<Name, Detail>` uses the Goa error name as its discriminant; each function exports the union of the errors its design declares.
- **SSE first, without automatic reconnection.** Streams are `fetch` responses exposed as async iterables that yield the method's whole event type. Reconnection, backoff and resumption stay with the application, which gets `lastEventId` to do it.
- **`number` for 64-bit integers, `string` for dates.** Integers are validated to be safe integers, so a value past 2^53 fails loudly instead of silently losing precision. `Format(FormatDate)` stays a `YYYY-MM-DD` string.
- **Hoist only what the design names.** Types declared with `Type` or `ResultType` go to `src/types.ts`. The user types Goa creates on its own (wrappers around inline payloads and results, around primitive union branches) are rendered inline.
- **Phasing and versioning.** Phase 1 ships types, schemas, the runtime and SSE; phase 2 adds REST; phase 3 adds WebSocket. Releases are `v0.x` until REST lands, `v1.0.0` when it does. Any `v2` uses the `/v2` module path suffix.

## Consequences

### Positive

- Goa errors are typed by name at every call site; the UIs' hand-written body parsing goes away.
- SSE events keep `id`, `event` and `retry`, and streams can resume and can carry headers and `POST` bodies.
- The design's validations hold on both sides of the wire, for requests and for responses.
- Types match the wire one to one, so a payload in the browser's network tab reads like the TypeScript type.
- The generated package has one dependency and no build step; the runtime can be read and debugged in place.

### Negative / breaking

- Consumers need Goa `v3.34` or later (the plugin compiles against that version's `expr` package) and `zod` `^4.1.0`.
- Migrating from the Speakeasy SDK changes property names to the wire's snake_case and error types from a generic SDK error to per-function unions.
- Until REST lands, a consumer runs two SDKs side by side.
- 64-bit integers past 2^53 cannot be represented; they fail validation.
- Every generated client carries its own copy of the runtime (about a thousand lines).

### Neutral

- Enumerations are closed: a client rejects a value its design does not list, so BFF and UI are expected to deploy together.
- Unknown attributes are dropped when decoding, so adding an attribute server-side does not break deployed clients.
- A top-level request attribute with a default is optional and omitted; the server applies the default.
- The plugin registers through Goa's original `codegen.RegisterPlugin` API, which Goa still supports alongside the newer `generator.RegisterPlugin`.

## Alternatives considered

1. **Keep Speakeasy.** No work, and none of the three costs go away; they are properties of the OpenAPI detour.
2. **Another OpenAPI generator** (openapi-ts, orval). Same detour: Goa's error content type, SSE mapping and validations are lost before the generator sees them.
3. **Publish the runtime to npm** instead of emitting it. A second artifact to version against the generator, for a thousand lines; a mismatch between the two would be a runtime failure in the browser.
4. **A class-based client.** Defeats tree shaking and is not how either UI calls its SDK today.
5. **The browser's `EventSource`.** It cannot send an `Authorization` header or a request body and hides HTTP errors, which rules out both BFFs.
6. **Convert property names to camelCase.** Needs a transform in every schema and makes the TypeScript type differ from what is on the wire; explicitly rejected.

## References

- Goa's SSE wire format: `http/codegen/templates/server_sse.go.tpl` and `partial/sse_format.go.tpl` in `goa.design/goa/v3`.
- Goa's design immutability check: `codegen/generator/lifecycle.go` (`verifyPreparedDesign`).
- First consumers: hyperion's `ui/src/queries/operations/stream-facility-group-operations.ts` and `ui/src/features/insights/queries/stream-facility-group-insights.ts`.
