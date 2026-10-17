import type * as z from 'zod';

import type { Client, EndpointSpec } from './client.js';
import type { RequestValidationError } from './errors.js';
import { type Result, unwrapAsync } from './result.js';
import { encode } from './schema.js';

/*
 * Query options for TanStack Query, without depending on it.
 *
 * A design that sets `Meta("typescript:query", "tanstack")` on its API gets,
 * beside every generated function, one-line exports built by the helpers
 * below. They return plain objects shaped like the options TanStack Query v5
 * takes (`useQuery`, `useMutation`, `QueryClient.fetchQuery`...), declared
 * structurally here: nothing in the runtime or the generated code imports
 * TanStack.
 */

/** QueryKey is a TanStack Query key. */
export type QueryKey = readonly unknown[];

/** KeyFlags tune the key of one generated query. */
export interface KeyFlags {
  /**
   * stream marks the key of a stream: `$stream: true` comes first in its
   * trailing object, always. Mutations never invalidate such a key, and a
   * stream never shares a key with a read on the same route. `streamFor`
   * sets it on its own; a stream's `keyFor` passes it.
   */
  readonly stream?: boolean | undefined;
  /**
   * discriminator tells apart queries of the same kind whose routes collide,
   * so that each keeps its own cache entry: 'method' puts `$method: '<VERB>'`
   * in the key's trailing object, after `$stream` when there is one.
   */
  readonly discriminator?: 'method' | undefined;
}

/** QueryFlags tune one generated query or stream. */
export interface QueryFlags extends KeyFlags {
  /**
   * required is true when the design requires a payload. The query then also
   * accepts `undefined`, which disables it (a dependent query).
   */
  readonly required?: boolean | undefined;
  /**
   * void is true for a read whose method has no result: its `queryFn`
   * resolves to `null`, since TanStack Query rejects `undefined` as data.
   */
  readonly void?: boolean | undefined;
}

/** QueryFunctionContextLike is the part of TanStack's query function context the helpers read. */
export interface QueryFunctionContextLike {
  signal: AbortSignal;
}

/** ReadQueryOptions are the options of a query: what `useQuery` and `fetchQuery` take. */
export interface ReadQueryOptions<T> {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContextLike) => Promise<T>;
  /** enabled is false when a required payload is `undefined`; absent otherwise. */
  enabled?: boolean;
}

/** QueryFunctionLike is any TanStack query function, such as what `streamedQuery` returns. */
export type QueryFunctionLike = (context: never) => unknown;

/** RefetchMode is how `streamedQuery` treats a refetch. */
export type RefetchMode = 'append' | 'reset' | 'replace';

/** StreamFn opens a stream for `streamedQuery`. */
export type StreamFn<TEvent> = (context: QueryFunctionContextLike) => Promise<AsyncIterable<TEvent>>;

/**
 * StreamedQueryParams is what a stream helper passes to `streamedQuery`: the
 * same union TanStack declares, since a `reducer` needs an `initialValue`.
 */
export type StreamedQueryParams<TEvent, TData> =
  | { streamFn: StreamFn<TEvent>; refetchMode?: RefetchMode; reducer?: never; initialValue?: never }
  | {
      streamFn: StreamFn<TEvent>;
      refetchMode?: RefetchMode;
      reducer: (acc: TData, event: TEvent) => TData;
      initialValue: TData;
    };

/**
 * StreamedQueryLike is the shape of TanStack's `experimental_streamedQuery`,
 * which the caller passes in.
 */
export type StreamedQueryLike<TEvent, TData, Q extends QueryFunctionLike> = (
  params: StreamedQueryParams<TEvent, TData>,
) => Q;

/** StreamQueryParams are the arguments of a stream query besides the client and the payload. */
export type StreamQueryParams<TEvent, TData, Q extends QueryFunctionLike> = {
  /** streamedQuery is `experimental_streamedQuery` from `@tanstack/react-query` (or `query-core`). */
  streamedQuery: StreamedQueryLike<TEvent, TData, Q>;
  refetchMode?: RefetchMode;
} & (
  | { reducer?: never; initialValue?: never }
  | { reducer: (acc: TData, event: TEvent) => TData; initialValue: TData }
);

/** StreamQueryOptions are the options of a stream query. */
export interface StreamQueryOptions<Q extends QueryFunctionLike> {
  queryKey: QueryKey;
  queryFn: Q;
  /** staleTime is Infinity: a stream stays open, so its data never goes stale. */
  staleTime: number;
  /** enabled is false when a required payload is `undefined`; absent otherwise. */
  enabled?: boolean;
}

/** MutationVariables is what a mutation takes: the payload, which may be omitted when the design allows it. */
export type MutationVariables<P> = undefined extends P ? P | void : P;

/** RawMutationVariables is what a raw-upload mutation takes. */
export interface RawMutationVariables<P> {
  payload: P;
  body: BodyInit;
}

/** InvalidateFilters are the filters a mutation passes to `invalidateQueries`; TanStack applies both. */
export interface InvalidateFilters {
  queryKey: QueryKey;
  predicate?: (query: { queryKey: QueryKey }) => boolean;
}

/** QueryClientLike is the part of TanStack's `QueryClient` a mutation uses. */
export interface QueryClientLike {
  invalidateQueries(filters: InvalidateFilters): Promise<unknown>;
}

/** MutationParams are the arguments of a mutation besides the client. */
export interface MutationParams {
  /** queryClient, when given, makes `onSuccess` invalidate the keys the mutation affects. */
  queryClient?: QueryClientLike | undefined;
}

/** MutationOptions are the options of a mutation: what `useMutation` takes. */
export interface MutationOptions<V, T> {
  mutationFn: (variables: V) => Promise<T>;
  /** onSuccess invalidates the affected keys; present only when a `queryClient` was given. */
  onSuccess?: (data: T, variables: V) => Promise<void>;
}

/** Invalidation computes the keys a mutation invalidates from its variables. */
export type Invalidation<V> = (variables: V) => QueryKey[];

/** CallResultLike is the success arm every generated function resolves to, among others. */
type ValueOf<R> = R extends { ok: true; value: infer T } ? T : never;

type CallOptions = { signal?: AbortSignal | undefined };

/** Endpoint is any generated function of the common shape (ADR 0003). */
type Endpoint<P, R> = (client: Client, payload: P, options?: CallOptions) => Promise<R>;

/** RawEndpoint is a generated raw-upload function. */
type RawEndpoint<P, R> = (client: Client, payload: P, body: BodyInit, options?: CallOptions) => Promise<R>;

/** PayloadOf is the local payload type of a request schema; `undefined` without one. */
export type PayloadOf<S extends z.ZodType | null> = S extends z.ZodType ? z.output<S> : undefined;

/** QueryBuilder is a generated `xyzQuery`; a void read's data is `null`. */
export type QueryBuilder<P, T, F extends QueryFlags> = F extends { required: true }
  ? (client: Client, payload: P | undefined) => ReadQueryOptions<F extends { void: true } ? null : T>
  : (client: Client, payload?: P) => ReadQueryOptions<F extends { void: true } ? null : T>;

/**
 * StreamBuilder is a generated stream `xyzQuery`. The payload is a required
 * parameter, `undefined` included, because `params` follows it.
 */
export type StreamBuilder<P, TEvent> = <TData = TEvent[], Q extends QueryFunctionLike = QueryFunctionLike>(
  client: Client,
  payload: P | undefined,
  params: StreamQueryParams<TEvent, TData, Q>,
) => StreamQueryOptions<Q>;

/** MutationBuilder is a generated `xyzMutation`. */
export type MutationBuilder<V, T> = (client: Client, params?: MutationParams) => MutationOptions<V, T>;

/*
 * Keys. The grammar below (ADR 0006) is shared with the generator, which
 * renders the same shapes into the state cards (`keySource` and
 * `pathKeyParts` in plugin/typescript/internal/tsgen/build_query.go): change
 * one, change the other.
 *
 *   key      = [...segments, trailing?]
 *   segments = the route split on '/', each parameter replaced by its raw
 *              wire value (null when there is none)
 *   trailing = { $stream?: true, $method?: VERB, ...inputs, $disabled?/$invalid?: true }
 */

/** pathSegments splits a goa path template, dropping empty segments. */
function pathSegments(template: string): string[] {
  return template.split('/').filter((s) => s !== '');
}

const PARAM = /\{\*?([^}]+)\}/g;
const WHOLE_PARAM = /^\{\*?([^}]+)\}$/;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/** paramProps maps the path parameter names of a spec to the request properties holding them. */
function paramProps(spec: EndpointSpec): Map<string, string> {
  const out = new Map<string, string>();
  for (const [prop, name] of Object.entries(spec.pathParams)) {
    out.set(name, prop);
  }
  return out;
}

/** property reads a request property of the wire payload; a non-object payload is its own single value. */
function property(wire: unknown, prop: string): unknown {
  return isRecord(wire) ? wire[prop] : wire;
}

/**
 * substitute fills the path parameters of a template with the raw wire
 * values (not percent-encoded; a wildcard stays one segment). It stops before
 * the first parameter with no value when `partial` is set, and puts `null` in
 * its place otherwise.
 */
function substitute(spec: EndpointSpec, template: string, wire: unknown, partial: boolean): unknown[] {
  const props = paramProps(spec);
  const value = (name: string): unknown => {
    const prop = props.get(name);
    return prop === undefined ? undefined : property(wire, prop);
  };
  const out: unknown[] = [];
  for (const segment of pathSegments(template)) {
    const whole = WHOLE_PARAM.exec(segment);
    if (whole) {
      const v = value(whole[1] as string);
      if (v === undefined || v === null) {
        if (partial) {
          return out;
        }
        out.push(null);
        continue;
      }
      out.push(v);
      continue;
    }
    if (!segment.includes('{')) {
      out.push(segment);
      continue;
    }
    // A parameter inside a segment: the segment becomes a string.
    let missing = false;
    const text = segment.replace(PARAM, (_m, name: string) => {
      const v = value(name);
      if (v === undefined || v === null) {
        missing = true;
        return '';
      }
      return String(v);
    });
    if (missing) {
      if (partial) {
        return out;
      }
      out.push(null);
      continue;
    }
    out.push(text);
  }
  return out;
}

/**
 * inputsOf collects every request input that is not in the path, in wire
 * form: query parameters, map query entries, header parameters and the JSON
 * body, under their request property names. Properties set to `undefined`
 * are left out. It returns `undefined` when there is nothing to collect.
 */
function inputsOf(spec: EndpointSpec, wire: unknown): unknown {
  if (wire === undefined || wire === null) {
    return undefined;
  }
  if (!isRecord(wire)) {
    // A scalar or array payload: it is the path parameter when the design
    // maps it there, and the trailing element otherwise.
    return Object.keys(spec.pathParams).length > 0 ? undefined : wire;
  }
  const out: Record<string, unknown> = {};
  const put = (prop: string, value: unknown): void => {
    if (value !== undefined) {
      out[prop] = value;
    }
  };
  for (const prop of Object.keys(spec.queryParams)) {
    put(prop, wire[prop]);
  }
  if (spec.mapQuery !== undefined) {
    if (spec.mapQuery === '') {
      for (const [k, v] of Object.entries(wire)) {
        put(k, v);
      }
    } else {
      put(spec.mapQuery, wire[spec.mapQuery]);
    }
  }
  for (const prop of Object.keys(spec.headerParams)) {
    put(prop, wire[prop]);
  }
  switch (spec.body?.kind) {
    case 'fields':
      for (const field of spec.body.fields) {
        put(field, wire[field]);
      }
      break;
    case 'attr':
      put(spec.body.attr, wire[spec.body.attr]);
      break;
    case 'whole':
      for (const [k, v] of Object.entries(wire)) {
        put(k, v);
      }
      break;
    default:
      // No body, or a raw body, which is not part of the key.
      break;
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

/** trailing adds the markers to the inputs of a key. */
function trailing(spec: EndpointSpec, inputs: unknown, options?: KeyFlags): unknown {
  const mark = markerOf(spec, options);
  if (!mark) {
    return inputs;
  }
  if (inputs === undefined) {
    return mark;
  }
  if (isRecord(inputs)) {
    return { ...mark, ...inputs };
  }
  // A scalar or array payload has no property to sit beside.
  return { ...mark, $value: inputs };
}

/**
 * markerOf is what comes first in the trailing object: `$stream` for a
 * stream, then `$method` for a discriminated key; undefined when neither.
 */
function markerOf(spec: EndpointSpec, options?: KeyFlags): Record<string, unknown> | undefined {
  if (!options?.stream && options?.discriminator !== 'method') {
    return undefined;
  }
  const mark: Record<string, unknown> = {};
  if (options.stream) {
    mark['$stream'] = true;
  }
  if (options.discriminator === 'method') {
    mark['$method'] = spec.method;
  }
  return mark;
}

/** isStreamKey reports whether a key is a stream's: its last element carries `$stream: true`. */
export function isStreamKey(key: QueryKey): boolean {
  const last = key[key.length - 1];
  return isRecord(last) && last['$stream'] === true;
}

/**
 * queryKeyFor returns the query key of a request: the path segments, with
 * each parameter replaced by its wire value, then one object holding every
 * other request input in wire form (omitted when there is none).
 *
 * `wire` is the payload after schema encoding. Credentials are never part of
 * it (they are not in the request types) and nothing of the client, such as
 * its base URL, enters the key.
 */
export function queryKeyFor(spec: EndpointSpec, wire: unknown, options?: KeyFlags): QueryKey {
  const key = substitute(spec, spec.path, wire, false);
  const rest = trailing(spec, inputsOf(spec, wire), options);
  if (rest !== undefined) {
    key.push(rest);
  }
  return key;
}

/**
 * keyPrefixFor returns the static segments of a path up to its first
 * parameter. A route that starts with a parameter has no such segment: it
 * gets the route with `null` for every parameter instead, never the empty
 * key, which would match every query.
 */
export function keyPrefixFor(spec: EndpointSpec): QueryKey {
  const out: string[] = [];
  for (const segment of pathSegments(spec.path)) {
    if (segment.includes('{')) {
      break;
    }
    out.push(segment);
  }
  if (out.length === 0) {
    return substitute(spec, spec.path, undefined, false);
  }
  return out;
}

/** hasParams reports whether a route has a path parameter. */
function hasParams(spec: EndpointSpec): boolean {
  return pathSegments(spec.path).some((s) => s.includes('{'));
}

/**
 * fallbackKeyFor is the key of a query that sends no request: the path with
 * `null` for every parameter, then a trailing object holding the
 * discriminator, if any, and a sentinel (`$disabled` for a required payload
 * that is `undefined`, `$invalid` for a payload that fails validation). No
 * real request has that key, so such a query never reads, or puts its error
 * on, data cached for another.
 */
function fallbackKeyFor(
  spec: EndpointSpec,
  sentinel: '$disabled' | '$invalid',
  options?: KeyFlags,
): QueryKey {
  const key = substitute(spec, spec.path, undefined, false);
  key.push({ ...markerOf(spec, options), [sentinel]: true });
  return key;
}

type Keyed = { ok: true; key: QueryKey } | { ok: false; key: QueryKey; error: RequestValidationError };

/** keyOf encodes a payload and derives its key; an invalid payload gets the `$invalid` fallback key. */
function keyOf(spec: EndpointSpec, schema: z.ZodType | null, payload: unknown, flags?: KeyFlags): Keyed {
  if (schema === null) {
    return { ok: true, key: queryKeyFor(spec, undefined, flags) };
  }
  const wire = encode(schema, (payload ?? {}) as z.output<z.ZodType>);
  if (!wire.ok) {
    return { ok: false, key: fallbackKeyFor(spec, '$invalid', flags), error: wire.error };
  }
  return { ok: true, key: queryKeyFor(spec, wire.value, flags) };
}

function disabledQueryFn(): Promise<never> {
  return Promise.reject(new Error('query disabled: payload is undefined'));
}

/**
 * keyFor builds a generated `xyzKey`: with a payload, the full key of that
 * request; without, an invalidation prefix that matches every request of
 * the method. For a route with parameters that prefix is the static
 * segments, which other methods under the same path share: it is not a
 * cache key. For a route without parameters it is the route followed by the
 * key's markers (`$stream`, `$method`) when it has some, so that it never
 * equals another query's exact key; TanStack matches the marker object as a
 * subset of a longer key's trailing object.
 */
export function keyFor<S extends z.ZodType | null>(
  spec: EndpointSpec,
  schema: S,
  flags: KeyFlags = {},
): (payload?: PayloadOf<S>) => QueryKey {
  return (payload) => {
    if (payload !== undefined) {
      return keyOf(spec, schema, payload, flags).key;
    }
    const prefix = keyPrefixFor(spec);
    const mark = markerOf(spec, flags);
    return hasParams(spec) || !mark ? prefix : [...prefix, mark];
  };
}

/**
 * queryFor builds a generated `xyzQuery`: `(client, payload?)` returning
 * `{ queryKey, queryFn }` for `useQuery`, `fetchQuery` and the like.
 *
 * When the design requires a payload, `undefined` is accepted too and
 * returns a disabled query (`enabled: false`), for dependent queries.
 */
export function queryFor<P, R extends Result<unknown, unknown>, const F extends QueryFlags>(
  fn: Endpoint<P, R>,
  spec: EndpointSpec,
  schema: z.ZodType | null,
  flags: F,
): QueryBuilder<P, ValueOf<R>, F> {
  const build = (client: Client, payload?: P): ReadQueryOptions<ValueOf<R>> => {
    if (payload === undefined && flags.required) {
      return { queryKey: fallbackKeyFor(spec, '$disabled', flags), queryFn: disabledQueryFn, enabled: false };
    }
    const keyed = keyOf(spec, schema, payload, flags);
    if (!keyed.ok) {
      const error = keyed.error;
      return { queryKey: keyed.key, queryFn: () => Promise.reject(error) };
    }
    if (flags.void) {
      return {
        queryKey: keyed.key,
        queryFn: async ({ signal }) => {
          await unwrapAsync(fn(client, payload as P, { signal }));
          return null as ValueOf<R>;
        },
      };
    }
    return {
      queryKey: keyed.key,
      queryFn: ({ signal }) => unwrapAsync(fn(client, payload as P, { signal })) as Promise<ValueOf<R>>,
    };
  };
  return build as QueryBuilder<P, ValueOf<R>, F>;
}

/** EventOf is the event type of the stream a generated function resolves to. */
type EventOf<R> = ValueOf<R> extends AsyncIterable<infer E> ? E : never;

/**
 * streamFor builds a generated stream `xyzQuery`: `(client, payload, params)`
 * returning `{ queryKey, queryFn, staleTime: Infinity }`, where `queryFn` is
 * what `params.streamedQuery` (TanStack's `experimental_streamedQuery`)
 * returns for a `streamFn` that opens the stream. `refetchMode`, `reducer`
 * and `initialValue` are passed through.
 *
 * Aborting the query's signal (unmount, `cancelQueries`) closes the stream.
 * The key always carries `$stream: true`, so mutations never invalidate it.
 */
export function streamFor<P, R extends Result<unknown, unknown>, const F extends QueryFlags>(
  fn: Endpoint<P, R>,
  spec: EndpointSpec,
  schema: z.ZodType | null,
  flags: F,
): StreamBuilder<P, EventOf<R>> {
  const build = (
    client: Client,
    payload: P | undefined,
    params: StreamQueryParams<EventOf<R>, unknown, QueryFunctionLike>,
  ): StreamQueryOptions<QueryFunctionLike> => {
    const { streamedQuery, ...rest } = params;
    const keyFlags: QueryFlags = { ...flags, stream: true };
    const open = (
      queryKey: QueryKey,
      streamFn: StreamFn<EventOf<R>>,
    ): StreamQueryOptions<QueryFunctionLike> => ({
      queryKey,
      queryFn: streamedQuery({ ...rest, streamFn } as StreamedQueryParams<EventOf<R>, unknown>),
      staleTime: Infinity,
    });
    if (payload === undefined && flags.required) {
      return { ...open(fallbackKeyFor(spec, '$disabled', keyFlags), disabledQueryFn), enabled: false };
    }
    const keyed = keyOf(spec, schema, payload, keyFlags);
    if (!keyed.ok) {
      const error = keyed.error;
      return open(keyed.key, () => Promise.reject(error));
    }
    return open(
      keyed.key,
      ({ signal }) => unwrapAsync(fn(client, payload as P, { signal })) as Promise<AsyncIterable<EventOf<R>>>,
    );
  };
  return build as unknown as StreamBuilder<P, EventOf<R>>;
}

/** InvalidationFlags tune a generated `xyzInvalidates`. */
export interface InvalidationFlags {
  /** raw is true for a raw upload, whose variables are `{ payload, body }`. */
  readonly raw?: boolean | undefined;
}

/**
 * invalidatesFor builds a generated `xyzInvalidates`: the keys a mutation
 * invalidates, from its variables. Each template is a path whose parameters
 * are filled in from the mutation's payload; the key stops before a
 * parameter the payload does not provide.
 */
export function invalidatesFor<S extends z.ZodType | null>(
  spec: EndpointSpec,
  schema: S,
  templates: readonly string[],
  flags: { raw: true },
): (variables?: RawMutationVariables<PayloadOf<S>>) => QueryKey[];
export function invalidatesFor<S extends z.ZodType | null>(
  spec: EndpointSpec,
  schema: S,
  templates: readonly string[],
  flags?: InvalidationFlags,
): (variables?: PayloadOf<S> | void) => QueryKey[];
export function invalidatesFor(
  spec: EndpointSpec,
  schema: z.ZodType | null,
  templates: readonly string[],
  flags: InvalidationFlags = {},
): (variables?: unknown) => QueryKey[] {
  return (variables) => {
    const payload = flags.raw ? (variables as RawMutationVariables<unknown> | undefined)?.payload : variables;
    let wire: unknown;
    if (schema !== null && (payload !== undefined || !flags.raw)) {
      const encoded = encode(schema, (payload ?? {}) as z.output<z.ZodType>);
      wire = encoded.ok ? encoded.value : undefined;
    }
    // A key stops at the first parameter the payload lacks; an empty key
    // would match every query, so it is dropped.
    return templates
      .map((template) => substitute(spec, template, wire, true))
      .filter((key) => key.length > 0);
  };
}

/**
 * mutationFor builds a generated `xyzMutation`: `(client, params?)` returning
 * `{ mutationFn }` for `useMutation`, plus an `onSuccess` that invalidates
 * the keys `invalidates` computes when `params.queryClient` is given. Stream
 * keys are left alone: invalidating an active stream would abort it and open
 * it again from the start.
 */
export function mutationFor<P, R extends Result<unknown, unknown>>(
  fn: RawEndpoint<P, R>,
  invalidates: Invalidation<RawMutationVariables<P>>,
  flags: { raw: true },
): MutationBuilder<RawMutationVariables<P>, ValueOf<R>>;
export function mutationFor<P, R extends Result<unknown, unknown>>(
  fn: Endpoint<P, R>,
  invalidates: Invalidation<MutationVariables<P>>,
): MutationBuilder<MutationVariables<P>, ValueOf<R>>;
export function mutationFor(
  fn: Endpoint<unknown, Result<unknown, unknown>> | RawEndpoint<unknown, Result<unknown, unknown>>,
  invalidates: Invalidation<never>,
  flags: InvalidationFlags = {},
): MutationBuilder<never, unknown> {
  return (client, params) => {
    const mutationFn = flags.raw
      ? (variables: unknown) => {
          const { payload, body } = variables as RawMutationVariables<unknown>;
          return unwrapAsync((fn as RawEndpoint<unknown, Result<unknown, unknown>>)(client, payload, body));
        }
      : (variables: unknown) =>
          unwrapAsync((fn as Endpoint<unknown, Result<unknown, unknown>>)(client, variables));
    const queryClient = params?.queryClient;
    if (!queryClient) {
      return { mutationFn };
    }
    return {
      mutationFn,
      onSuccess: async (_data, variables) => {
        await Promise.all(
          invalidates(variables as never).map((queryKey) =>
            queryClient.invalidateQueries({ queryKey, predicate: (query) => !isStreamKey(query.queryKey) }),
          ),
        );
      },
    };
  };
}
