# ADR 0006: TanStack Query options, opt-in, without a TanStack dependency

- **Date:** 2026-10-15
- **Status:** Accepted
- **Supersedes:** none; builds on ADR 0003 (one call shape) and ADR 0005 (WebSocket methods)

## Context

The UIs that consume a generated client keep server data in the TanStack Query cache. The team's React State Playbook names two properties a generated client can serve directly:

1. **Server state lives in the Query cache, and mutations invalidate it.** Every read goes through `useQuery` under a key, every write through `useMutation`, and a write marks the reads it affects stale. Today each UI writes the `queryKey`/`queryFn` pairs, the mutation wrappers and the invalidation lists by hand, for every endpoint, and keeps them in step with the design.
2. **Discoverability through state cards.** For every piece of state, a short card says who owns it, where it comes from (its key), whether the client may write it, and what invalidates it. Written by hand, the cards drift from the code they describe.

The design already holds everything both need: the verb says read or write, the route gives a stable key, the payload mapping says which inputs distinguish two requests, and ADR 0003 gives every function one shape a generic helper can wrap. What it does not hold is the policy: which writes affect which reads.

TanStack Query v5 ships `queryOptions`, `mutationOptions` and `experimental_streamedQuery`; adapters exist for React, Vue, Solid and Svelte. Its options are plain objects, so a generator can produce them without calling TanStack.

## Decision

**`Meta("typescript:query", "tanstack")` on the API generates, beside every function, one-line exports that build TanStack Query options through dependency-free runtime helpers. Without the meta the generated functions are unchanged.**

### Classification

- **query**: `GET` and `HEAD` functions.
- **stream**: SSE functions, and WebSocket methods without a `StreamingPayload` (server streams).
- **mutation**: every other verb, raw uploads included.
- **none**: raw downloads (`SkipResponseBodyEncodeDecode`: the body stays on the response, which a cache cannot hold), WebSocket methods that stream a payload, and anything the SDK skips. They are listed with the reason at the bottom of the service barrel.
- A mixed method yields a query for its plain function and a stream for its SSE function.
- `Meta("typescript:query:kind", "query" | "mutation" | "none")` on a method overrides the classification (a `POST` used as a read). `goa gen` fails on an override that cannot work: a query or mutation over a stream, a query over a raw upload or download, query options for a method the SDK skips.

### Output

- `src/<service>/<method>.query.ts`, re-exported by the service barrel. Per read or stream: `xyzQuery` and `xyzKey`; per mutation: `xyzMutation` and `xyzInvalidates`. A mixed method's stream exports are `xyzStreamQuery`/`xyzStreamKey`, after its `xyzStream` function. The endpoint file exports its `EndpointSpec` constant for the query file to use.
- Every export is one line calling a runtime helper (`queryFor`, `streamFor`, `keyFor`, `mutationFor`, `invalidatesFor` in `runtime/src/query.ts`), so the logic is in the runtime and tested once.
- Every export carries a JSDoc state card: Owner (server), Source (the key shape, e.g. `["things", id, { tags?, verbose? }]`), Writable (no), Mutations & invalidation (the generated mutations whose invalidation matches the key); for a mutation, Invalidates. Collisions are recorded there too.

### Runtime helpers

- Nothing in the runtime or the generated code imports TanStack. `query.ts` declares the minimal structural types TanStack v5 accepts (`QueryKey`, the options objects, a `QueryClientLike` with `invalidateQueries`), and the toy example compiles them against `@tanstack/query-core` and drives a real `QueryClient`.
- **Keys** are `[...pathSegments, inputs?]`: the route split on `/`, each `{name}` or `{*name}` replaced by the raw wire value of its property (a wildcard stays one segment), then one object of every other input in wire form, under the request property names: query parameters, map query entries, header parameters, the JSON body's properties (or the scalar payload as is). The object is omitted when empty. Credentials never enter a key (keys are built from the encoded payload, never from the prepared request), nor does anything of the client.
- **`keyPrefixFor(spec)`** is the static segments up to the first parameter; `xyzKey()` without a payload returns it.
- **Queries**: `xyzQuery(client, payload)` returns `{ queryKey, queryFn }`, `queryFn: ({ signal }) => unwrapAsync(fn(client, payload, { signal }))`, and nothing else (no `staleTime`, `retry` or `gcTime`). The payload parameter follows ADR 0003. When the design requires a payload, `undefined` is accepted and returns `enabled: false` with a `queryFn` that rejects (dependent queries). A payload that fails encoding is keyed by the prefix and its `queryFn` rejects with the `RequestValidationError`.
- **Streams**: `xyzQuery(client, payload, { streamedQuery, refetchMode?, reducer?, initialValue? })` returns `{ queryKey, queryFn, staleTime: Infinity }`, where `queryFn` is what the injected `streamedQuery` returns for a `streamFn` that opens the stream with the query's signal. The types line up with TanStack's own so that `reducer: (acc, event) => ...` types `event` as the method's event, and a reducer without an `initialValue` does not compile.
- **Mutations**: `xyzMutation(client, { queryClient? })` returns `{ mutationFn }`, plus `onSuccess` when a `queryClient` is given, which invalidates every key in the mutation's list and awaits them. A raw upload's variables are `{ payload, body }`. `xyzInvalidates(variables)` computes the list for code that wires invalidation itself.
- **Default invalidation**: the key of the mutation's own path with trailing parameters stripped (`PUT /things/{id}` and `POST /things` invalidate `["things"]`; `DELETE /things/{id}/labels/{lid}` invalidates `["things", id, "labels"]`), parameters in the kept part filled from the variables. `Meta("typescript:invalidates", "a, b")` replaces it with the prefixes of the named queries or streams of the same service; `""` disables it.

### Collisions

Across every service, two queries or streams whose routes are equal once parameter names are ignored would share keys. The `GET` member keeps its clean key; every other member gets `$method: "<VERB>"` first in its trailing object (created if needed); if no member is a `GET`, all are discriminated. The stream of a mixed method shares its plain function's `GET` route and is the one discriminated.

## Consequences

### Positive

- A read is `useQuery(toy.getThingQuery(client, { id }))`, a write `useMutation(toy.upsertThingMutation(client, { queryClient }))`, with keys and invalidation derived from the design and kept in step by regeneration.
- The state cards are generated, so they say what the code does.
- No new dependency: the generated package still depends on `zod` alone, works with any TanStack adapter, and does not pin a TanStack version.
- Keys are readable in the devtools (`["things", "abc", { verbose: true }]`) and prefix-match the way TanStack invalidation expects.

### Negative / breaking

- Every generated client carries `runtime/query.ts` (about 500 lines, mostly types), and its export names (`QueryKey`, `MutationOptions`, ...) join the names a design type cannot take, whether or not the layer is enabled; tree shaking drops the code when unused.
- Default invalidation is coarse: a write under `/things` invalidates every key under `["things"]`. That is the safe direction; `typescript:invalidates` narrows it.
- Keys ignore the service and the client: two services serving the same path, or one UI talking to two deployments through two clients, share cache entries. A UI with two backends uses two `QueryClient`s.
- A refetch of a stream opens a new stream from the start; it does not resume.
- Only routes that are equal up to parameter names are detected as colliding. A static segment that equals a parameter value (`GET /things/search` and `GET /things/{id}` with id `"search"`) is not.

### Neutral

- A disabled dependent query is keyed by its route with `null` for each parameter, not by the bare prefix: the prefix can equal another method's full key (`["things"]` is also `listThings`'s), and a disabled observer still reads the cache entry of its key.
- The stream helpers take `streamedQuery` from the caller on every call; the generated code cannot import it without importing TanStack. The structural types follow its 5.86+ shape (`streamFn`).

## Alternatives considered

1. **Generated hooks** (`useGetThing`). They tie the package to React and to one adapter, need a way to find the client (a context the package would have to own), and multiply the API surface by the hook variants (`useSuspenseQuery`, `useQueries`, ...). Options objects work with every adapter and every variant; the hook is a one-liner in the application.
2. **Importing `queryOptions` and `experimental_streamedQuery` from TanStack.** `queryOptions` would add the `DataTag` brand that types `getQueryData`, but makes `@tanstack/query-core` (or an adapter) a dependency of the generated package and pins its version range; `streamedQuery` is still experimental and has changed shape within v5 (`queryFn` became `streamFn`, with `reducer` and `initialValue`, in 5.86). The application wraps with `queryOptions(...)` when it wants the brand.
3. **Verb-first keys** (`["GET", "things", id]`). Every key would carry the verb even though collisions are rare, and invalidating a resource would need one call per verb. `$method` appears only when needed.
4. **Service and method keys** (`["toy", "getThing", payload]`). Stable under route changes, but invalidation by resource stops working: a write to `/things/{id}` cannot name the keys of every read under `/things` without the generator listing them, and two methods on one resource never share a prefix. The route is the resource, which is what invalidation is about.
5. **Injecting `streamedQuery` once, on the client** (`createClient({ streamedQuery })`). One place to configure, but the client would carry a TanStack concern into every call site that does not use it, and the stream helpers' types could no longer infer the `TData` of a reducer from the call.

## References

- `runtime/src/query.ts`, `runtime/test/query.test.ts`.
- `plugin/typescript/internal/tsgen/build_query.go`, `templates/endpoint_query.ts.tpl`, `testdata/golden/Query`.
- `examples/toy/ts/query.test.ts`, run against `@tanstack/query-core` 5.104.3.
- TanStack Query v5: `streamedQuery.ts`, `QueryClient.invalidateQueries`, `QueryObserverOptions`, `MutationOptions`.
- ADR 0003, ADR 0005.
