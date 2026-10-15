# ADR 0005: WebSocket methods over the native WebSocket API

- **Date:** 2026-10-14
- **Status:** Accepted
- **Supersedes:** none; carries out ADR 0001, "Phasing and versioning" (phase 3), and applies ADR 0003

## Context

Until now the generator skipped WebSocket methods ("WebSocket endpoints are not generated yet"), and the UIs that call them use a WebSocket library and borrow only the parsers of their SDK (ADR 0001, Context). Phase 3 generates them.

goa's WebSocket protocol, as its `v3.34` templates implement it (`http/codegen/templates/websocket_*.go.tpl`, `partial/websocket_upgrade.go.tpl`):

- The upgrade request is a `GET` without a body; the non-streaming `Payload` travels as path and query parameters and headers. goa refuses a body there.
- Every message is one JSON text frame (`WriteJSON`): the result body type from the server, the streaming payload from the client. The streaming body is the `StreamingPayload` as is: goa offers no `Body` mapping for it.
- A client that streams a payload ends its input with the JSON `null`, which the server reads as end of file. A client-streaming method (`StreamingPayload` and `Result`) then writes its one result and drops the connection without a close frame (`SendAndClose` defers `conn.Close()`).
- The server ends a stream with a normal close, code 1000; goa's Go client reads that as end of file.
- The server upgrades lazily, on the method's first `Send`, `Recv` or `Close`, after the endpoint's security handler has run. An error before then, including a declared goa error, is an ordinary HTTP response to the upgrade request.
- A viewed result names its view in the `goa-view` header of the upgrade response. goa refuses a runtime view choice on a method with a `StreamingPayload`.
- A method with both `Result` and `StreamingResult` must use `ServerSentEvents()`: there is no mixed WebSocket method.

The browser `WebSocket` API constrains what a client can do with that protocol. It sends no custom header. It reports a refused upgrade as an `error` event followed by a close (code 1006), without the status or the body of the response. It shows no response header. Its `close()` accepts code 1000 or 3000-4999 only.

## Decision

**A WebSocket method becomes `fn(client, payload?, options?)` resolving, once the socket is open, to `CallResult<WebSocketStream<Recv, Send>, RequestValidationError | TransportError>`.** The payload follows ADR 0003 (`RequestSpec.Optional`); credentials are stripped from it as for every other function.

- **`WebSocketStream`** (`runtime/src/websocket.ts`), modeled on `EventStream`, is an async iterable of the messages the server sends, each validated by its schema. `send(msg)` encodes through the schema and returns `Result<void, RequestValidationError | NetworkError>` rather than throwing. `end()` sends goa's `null`. `close(code?, reason?)` closes; `closed` resolves with `{ code, reason, wasClean }`. `Send` is `never` for a server stream, so `send` does not type-check there; `end()` is on every stream and harmless where the server does not read.
- **Iteration** ends on close code 1000, and on a close the client started (`close`, leaving the loop, aborting). Any other close throws a `NetworkError` that carries `closeCode` and `closeReason`. A message that fails validation throws `ResponseValidationError` and closes the socket. For a client-streaming method the stream expects exactly one message: after it the iteration ends and the client closes the socket, so goa's frameless drop is not mistaken for a failure.
- **Types.** `Recv` is the streaming result, or the result of a client stream; `Send` is the streaming payload on its request side (ADR 0004). Unnamed ones are declared in the method's file as `<Method>Result` and `<Method>StreamingPayload`. A method that declares no result has `Recv = never`.
- **Credentials and headers.** Query credentials go through the same `applyCredential` code as `fetch` requests: the URL, headers and body assembly is factored out of `send` into `prepareRequest`, shared by both transports. `ClientOptions.webSocket`, a factory `(url, protocols, init: { headers }) => WebSocket` defaulting to `globalThis.WebSocket`, replaces the constructor the way `ClientOptions.fetch` replaces fetch. `init.headers` holds what the call would send as headers (client headers, header parameters, header credentials, per-call headers), so a factory built on a library that can send them (`ws`, undici's global `WebSocket` in Node 22+) does. The function's documentation lists the header credentials and header parameters a browser cannot send.
- **Handshake failures** are `NetworkError`s, with the close code and reason when there are any, never a typed `GoaError`. The error union is `RequestValidationError | TransportError`, and the function's documentation lists the errors the design declares instead.
- **`result.response`** is a stand-in: an empty `200` `Response`. `CallResult` requires one on success, the browser exposes nothing of the real handshake response, and the `Response` constructor rejects status 101.
- **Timeout and abort.** `timeoutMs` bounds the wait for `open` (`TimeoutError`). Aborting `signal` before `open` fails the call with `AbortError`; after, it closes the socket with code 1000 and the loop throws `AbortError`, as `EventStream` does.
- **URL.** `buildURL`, resolved against `globalThis.location` when there is one (a relative `baseURL` works in a browser), then `http:` to `ws:` and `https:` to `wss:`.
- **Out of scope:** reconnection, binary frames, `bufferedAmount` backpressure, ping and pong, exposing the view. Middleware does not apply: it wraps `fetch`, and there is no `Request` or `Response` to give it.
- **Skipped**, with the reason in the service barrel: a client-streaming method whose success `Response` sends the result as one attribute (`Body("attr")`), since that frame is not the result.

## Consequences

### Positive

- WebSocket methods get the same call shape, validation and codecs as every other function: a wrapper typed for ADR 0003 accepts them, dates arrive as `Date`, and an invalid message is caught on either side of the wire.
- Server, client and bidirectional streams share one type, whose `Send` parameter keeps a server stream from sending at compile time.
- Node callers, and tests, authenticate with header credentials through the factory, without a dependency in the generated package.

### Negative / breaking

- In a browser, header credentials do not reach the server, and the default goa JWT scheme is a header. A service that wants browser WebSocket clients maps the credential to a query parameter or relies on cookies.
- Declared goa errors cannot be told apart from a network failure, and `result.response` carries nothing; code that branches on `result.error.kind === "goa"` never sees one from a WebSocket function.
- `send` reports a closed socket as a `NetworkError` beside the validation error, where the browser API would drop the message silently.
- Every generated client carries one more runtime file (about 400 lines).

### Neutral

- A viewed result decodes whatever view the server picked, because the schema makes view-dependent attributes optional; the view itself is not exposed.
- goa's server `Close()` does not close the connection when it cannot write its close frame, for instance after the client closed first and gorilla's close handler answered. A client then waits for the server to drop the TCP connection before `closed` resolves (iteration has already ended). The toy server drops its connections itself; a real service should too.

## Alternatives considered

1. **Subprotocol tricks to carry a token** (`new WebSocket(url, ["bearer", token])`). goa's upgrader does not read `Sec-WebSocket-Protocol` as a credential, so it would need server changes; it also leaks the token into logs of proxies that record the header.
2. **Fetching a pre-flight request** to learn the handshake error. A second request is not the handshake: it can succeed where the upgrade fails and doubles the cost of every connection.
3. **A library such as `ws` as a dependency** of the generated package. It does not run in a browser, and ADR 0001 keeps `zod` the only dependency. The factory lets Node callers bring it.
4. **Typed `GoaError`s from a factory that can see the response.** No portable API exposes the handshake response, `ws` included without its non-standard `unexpected-response` event; the union stays honest instead.
5. **A `Result` without `response`** for WebSocket functions. It would not satisfy `CallResult`, and generic wrappers written for ADR 0003 would need a second signature.

## References

- goa `v3.34.0`: `http/codegen/templates/websocket_send.go.tpl`, `websocket_recv.go.tpl`, `websocket_close.go.tpl`, `partial/websocket_upgrade.go.tpl`, `server_handler_init.go.tpl`; `expr/http_endpoint.go` (`Validate`), `expr/http_body_types.go` (`httpStreamingBody`).
- `runtime/src/websocket.ts`, `runtime/src/client.ts` (`prepareRequest`, `ClientOptions.webSocket`), `plugin/typescript/internal/tsgen/build_websocket.go`, `templates/partial/websocket_func.ts.tpl`.
- `examples/toy/ts/websocket.test.ts`, `examples/toy/cmd/toy/websocket.go`.
- ADR 0001, ADR 0003, ADR 0004.
