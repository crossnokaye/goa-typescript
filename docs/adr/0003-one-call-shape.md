# ADR 0003: One call shape for every function

- **Date:** 2026-10-12
- **Status:** Accepted
- **Supersedes:** none; refines ADR 0001, "Functional only"

## Context

ADR 0001 settled on `fn(client, payload, options?)`. The generator then made an exception: a method with no `Payload` (or with nothing left of it once credentials move to the client's `auth`) got `fn(client, options?)`. That gave two shapes, chosen by the design, and `payload` was always required when present:

```ts
listThings(client, options?)          // no Payload
getConfig(client, payload, options?)  // Payload, every attribute optional
getConfig(client)                     // compile error
```

Two problems followed.

- **Generic code must branch on arity.** A shared wrapper (adding auth, unwrapping, feeding TanStack Query) has to carry both function types and decide at run time which one it holds, since the second argument is a payload for one function and the options for another. Nothing in the type system tells them apart: both are plain objects.
- **A compatible design change breaks callers.** Adding the first optional attribute to a payload-less method turns `fn(client, options?)` into `fn(client, payload, options?)`. The service considers that backward compatible; every existing caller that passed options now passes them as the payload, or fails to compile because the payload is required.

A third, smaller irritation: a method whose attributes are all optional, or all defaulted, still needs `{}` at every call site.

## Decision

**Every function is `fn(client, payload?, options?)`. The payload is always the second parameter, and whether it may be omitted follows from the design.**

| Payload (after credentials are stripped) | Parameter |
|---|---|
| an object with at least one required attribute, or a scalar | `payload: <Method>Payload` |
| an object with no required attribute | `payload?: <Method>Payload`; the function encodes `payload ?? {}` |
| none | `payload?: undefined`; the function ignores it |

- A payload-less call that passes options is `fn(client, undefined, options)`. The placeholder is the cost of the single shape; in exchange, `fn(client, { signal })` is a compile error instead of a request with `signal` serialized into it.
- A raw-body function (`SkipRequestBodyEncodeDecode`) is `fn(client, payload, body, options?)`. TypeScript forbids an optional parameter before a required one, so `payload` is required there whatever the design says: `payload: undefined` when the method has none, `payload: <Method>Payload` when every attribute is optional. The function's documentation says so.
- The decision is made while building the spec (`RequestSpec.Optional`), not in the templates, which only read it.
- The runtime exports `EndpointError = GoaError<string, unknown> | ClientError`, the type every function's error union narrows to, so that a wrapper can be typed `(client, payload, options?) => Promise<CallResult<Res, EndpointError>>` once.

## Consequences

### Positive

- One function type covers every endpoint. Wrappers, query-options aliases and error formatters are written once, without casts or run-time branching.
- Adding the first optional attribute to a method, or removing the last required one, no longer changes the shape of the call. A payload-less method that gains an optional attribute goes from `payload?: undefined` to `payload?: X`: a call that passed `undefined` still compiles.
- An all-optional payload may be omitted: `getConfig(client)`.

### Negative / breaking

- `fn(client, options)` on a payload-less method no longer compiles. Such calls become `fn(client, undefined, options)`. Nothing had been published.
- The placeholder reads oddly at first sight. The README explains it once, and the type error for the old form points at it.
- The raw-body exception is a wart. It is confined to a shape that is already special (a fourth argument) and is called out in the generated documentation.

### Neutral

- An omitted all-optional payload encodes as `{}`, which is what callers wrote by hand before. Request validation is unchanged.
- A scalar payload (`Payload(String)`) stays required: there is no empty value to stand in for it.

## Alternatives considered

1. **Keep two shapes, make an all-optional payload optional.** Fixes the `{}` irritation only. `fn(client, options)` would then type-check against an all-optional payload and send the options as the request, and generic code would still branch on arity.
2. **Options first: `fn(client, options, payload?)`.** One shape without a placeholder, but every call that passes a payload and no options (the common case) would carry `undefined` instead, and the shape would differ from ADR 0001 and from the SDKs the UIs use today.
3. **A single options object: `fn(client, { payload, signal })`.** One shape, no placeholder, but the payload is buried under a key at every call site, and `signal` or `headers` could collide with attribute names.
4. **Overloads on the payload-less functions**, accepting both `(client, options)` and `(client, undefined, options)`. The two forms are indistinguishable at the type level once a wrapper erases the function type, so the generic-code problem stays.

## References

- GitHub issue #3.
- `plugin/typescript/internal/tsgen/build_request.go` (`RequestSpec.Optional`), `templates/partial/rest_func.ts.tpl`, `templates/partial/sse_func.ts.tpl`.
- `runtime/src/errors.ts` (`EndpointError`).
- ADR 0001, "Functional only".
