# ADR 0002: Response metadata rides beside the value

- **Date:** 2026-10-11
- **Status:** Accepted
- **Supersedes:** none

## Context

Phase 2 adds functions for unary (REST) endpoints. A caller sometimes needs more than the decoded result: the status code when a method declares several success responses, a header the design did not map, the undecoded body of a download. The function has to put that somewhere.

The Speakeasy SDKs the UIs use today return the bare value and offer no way to reach the response. Other generators wrap every result: `{ data, response }`, `{ data, status, headers }`.

Goa already has an answer for most metadata. A design maps a response header onto a result attribute (`Header("etag:ETag")`), and a status onto an attribute value (`Tag("outcome", "created")`). What a design maps is part of the result type, on the server and in goa's own Go client.

## Decision

**The carrier rule: a function's value is the bare goa type. Transport metadata rides beside it, never around it.**

- Generated functions return `CallResult<T, E>`: a `Result<T, E>` plus `response`, the fetch `Response`.

  ```ts
  type CallResult<T, E> =
    | { ok: true; value: T; response: Response }
    | { ok: false; error: E; response?: Response };
  ```

- On success `response` is always set. On failure it is set whenever a response arrived (a goa error, an unexpected response, a response that fails validation) and absent otherwise (network failure, abort, timeout, a request that fails validation).
- `CallResult` is assignable to `Result`, so `unwrap` and `unwrapAsync` are unchanged and return the bare value.
- `GoaError` and `UnexpectedResponseError` already carry `response`. The duplication is deliberate: `result.response` is one place to look whatever the failure, without first finding out which error class it is.
- What the design maps stays in the value. A header mapped to an attribute fills that attribute; a response's `Tag` sets the tag attribute, as goa's Go client does. Several success statuses share one value type, and `result.response.status` says which response it was. There are no per-status union types.
- The body of a response is consumed by decoding, with one exception: for `SkipResponseBodyEncodeDecode` it is left unread on `result.response`, for the caller to take with `blob()`, `arrayBuffer()` or `body`.
- The rule holds for every transport. For Server-Sent Events the stream is the carrier of per-event metadata (`lastEventId`, `retry`), and SSE functions return `CallResult` too: the same `Response` is at `result.response` and `result.value.response`. This changes the return type phase 1 generated; nothing had been published. For WebSocket the socket will be the carrier.

## Consequences

### Positive

- Call sites read like the design: `result.value` is the `Result`, with no `.data` hop, and a function can be passed to `unwrapAsync` and used as a TanStack Query `queryFn` directly.
- Status and headers are always reachable, for every function, in the same place, on success and on failure.
- Adding metadata later (timing, a request ID) means adding a field beside the value; no value type changes.

### Negative / breaking

- The SSE functions of phase 1 now return `CallResult` instead of `Result`. Code that only reads `ok`, `value` and `error` is unaffected.
- `unwrap` drops the response. A caller that wants both keeps the result.
- The `Response` is live: on the raw-download path its body can be read once, and holding on to it holds the connection.

### Neutral

- A result object now has three keys instead of two. `{ ok, value, response }` is not a `Result` literal, so the runtime builds it directly rather than through `ok()`.

## Alternatives considered

1. **An envelope: `value` is `{ data, response }` or `{ data, status, headers }`.** Rejected. Goa already maps metadata onto attributes, so the envelope would be a second, competing place for the same facts. Every call site pays a `.data` hop to serve the few that want a header. And it does not extend: a WebSocket has no per-message metadata to put in an envelope, so streams would need a different shape anyway.
2. **Per-status union types** (`{ status: 200; value: A } | { status: 201; value: B }`). Rejected. Goa gives a method one result type whatever the status, and the status alone identifies the response (goa rejects two responses with the same status). A union would invent a distinction the design does not make.
3. **The response only on the error classes.** That is what phase 1 had. It leaves the success path without status or headers, which the multi-status and raw-download cases need.
4. **A second function per endpoint that returns the response** (`getThingRaw`). Twice the surface for the same call.

## References

- `runtime/src/result.ts` (`CallResult`), `runtime/src/schema.ts` (`decodeResponse`).
- ADR 0001, "Functional only" and "Typed Goa errors".
