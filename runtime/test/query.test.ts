import * as z from 'zod';
import { describe, expect, expectTypeOf, it } from 'vitest';

import {
  type CallResult,
  type Client,
  createClient,
  dateTime,
  type EndpointSpec,
  invalidatesFor,
  keyFor,
  keyPrefixFor,
  mutationFor,
  type InvalidateFilters,
  isStreamKey,
  type QueryKey,
  queryFor,
  queryKeyFor,
  RequestValidationError,
  type RequestOptions,
  type StreamedQueryParams,
  streamFor,
} from '../src/index.js';

const spec = (over: Partial<EndpointSpec>): EndpointSpec => ({
  method: 'GET',
  path: '/things',
  pathParams: {},
  queryParams: {},
  headerParams: {},
  accept: 'application/json',
  security: [],
  ...over,
});

const getThingSpec = spec({
  path: '/api/things/{id}',
  pathParams: { id: 'id' },
  queryParams: { tags: 'tag', verbose: 'verbose', since: 'since' },
  headerParams: { trace: 'X-Trace-Id' },
  security: [{ scheme: 'jwt', kind: 'bearer', in: 'header', name: 'Authorization' }],
});

const GetThingPayload = z.object({
  id: z.string(),
  tags: z.array(z.string()).optional(),
  verbose: z.boolean().optional(),
  since: dateTime().optional(),
  trace: z.string().optional(),
});
type GetThingPayload = z.output<typeof GetThingPayload>;

const client: Client = createClient({ baseURL: 'http://toy.test/base', auth: 'secret-token' });

const ok = <T>(value: T): CallResult<T, never> => ({ ok: true, value, response: new Response(null) });

describe('queryKeyFor', () => {
  it('puts path parameters in place and every other input in a trailing object, in wire form', () => {
    const key = queryKeyFor(getThingSpec, {
      id: 'a/b c',
      tags: ['x', 'y'],
      verbose: false,
      since: '2026-01-02T03:04:05.000Z',
      trace: 't1',
    });
    expect(key).toEqual([
      'api',
      'things',
      'a/b c', // raw, not percent-encoded
      { tags: ['x', 'y'], verbose: false, since: '2026-01-02T03:04:05.000Z', trace: 't1' },
    ]);
  });

  it('omits undefined properties and the trailing object when nothing is left', () => {
    expect(queryKeyFor(getThingSpec, { id: 'a', verbose: undefined })).toEqual(['api', 'things', 'a']);
  });

  it('keeps a wildcard as one segment and non-string path values as they are', () => {
    const files = spec({ path: '/files/{*path}', pathParams: { path: 'path' } });
    expect(queryKeyFor(files, { path: 'a/b/c.txt' })).toEqual(['files', 'a/b/c.txt']);
    const page = spec({ path: '/pages/{n}/v{rev}', pathParams: { n: 'n', rev: 'rev' } });
    expect(queryKeyFor(page, { n: 3, rev: 2 })).toEqual(['pages', 3, 'v2']);
  });

  it('collects a map query under its property, or spreads it when it is the payload', () => {
    const named = spec({ queryParams: { q: 'q' }, mapQuery: 'filters' });
    expect(queryKeyFor(named, { q: 'x', filters: { a: '1', b: '2' } })).toEqual([
      'things',
      { q: 'x', filters: { a: '1', b: '2' } },
    ]);
    const whole = spec({ mapQuery: '' });
    expect(queryKeyFor(whole, { a: '1', b: '2' })).toEqual(['things', { a: '1', b: '2' }]);
  });

  it('collects the JSON body: fields, an attribute or a whole object', () => {
    const fields = spec({ method: 'POST', body: { kind: 'fields', fields: ['name', 'kind'] } });
    expect(queryKeyFor(fields, { name: 'n', kind: 'k' })).toEqual(['things', { name: 'n', kind: 'k' }]);
    const attr = spec({ method: 'POST', body: { kind: 'attr', attr: 'filter' } });
    expect(queryKeyFor(attr, { filter: { kinds: ['a'] } })).toEqual(['things', { filter: { kinds: ['a'] } }]);
    const whole = spec({ method: 'POST', body: { kind: 'whole' } });
    expect(queryKeyFor(whole, { a: 1 })).toEqual(['things', { a: 1 }]);
  });

  it('puts a scalar or array payload in as is, unless it is the path parameter', () => {
    const body = spec({ method: 'POST', body: { kind: 'whole' } });
    expect(queryKeyFor(body, 'text')).toEqual(['things', 'text']);
    expect(queryKeyFor(body, [1, 2])).toEqual(['things', [1, 2]]);
    const path = spec({ path: '/things/{id}', pathParams: { '': 'id' } });
    expect(queryKeyFor(path, 'abc')).toEqual(['things', 'abc']);
  });

  it('leaves a raw body out', () => {
    const raw = spec({ method: 'PUT', body: { kind: 'raw' }, headerParams: { type: 'Content-Type' } });
    expect(queryKeyFor(raw, { type: 'text/plain' })).toEqual(['things', { type: 'text/plain' }]);
  });

  it('puts the discriminator first in the trailing object, creating it when needed', () => {
    const head = spec({ method: 'HEAD', path: '/things/{id}', pathParams: { id: 'id' }, queryParams: { v: 'v' } });
    const key = queryKeyFor(head, { id: 'a', v: true }, { discriminator: 'method' });
    expect(key).toEqual(['things', 'a', { $method: 'HEAD', v: true }]);
    expect(Object.keys(key[2] as object)[0]).toBe('$method');
    expect(queryKeyFor(head, { id: 'a' }, { discriminator: 'method' })).toEqual(['things', 'a', { $method: 'HEAD' }]);
    const scalar = spec({ method: 'POST', body: { kind: 'whole' } });
    expect(queryKeyFor(scalar, 'x', { discriminator: 'method' })).toEqual(['things', { $method: 'POST', $value: 'x' }]);
  });

  it('marks a stream key with $stream first, then $method', () => {
    const feed = spec({ path: '/feed', queryParams: { kind: 'kind' } });
    const key = queryKeyFor(feed, { kind: 'a' }, { stream: true });
    expect(key).toEqual(['feed', { $stream: true, kind: 'a' }]);
    expect(Object.keys(key[1] as object)[0]).toBe('$stream');
    expect(queryKeyFor(feed, {}, { stream: true })).toEqual(['feed', { $stream: true }]);
    const post = spec({ method: 'POST', path: '/feed', body: { kind: 'fields', fields: ['kind'] } });
    const both = queryKeyFor(post, { kind: 'a' }, { stream: true, discriminator: 'method' });
    expect(both).toEqual(['feed', { $stream: true, $method: 'POST', kind: 'a' }]);
    expect(Object.keys(both[1] as object)).toEqual(['$stream', '$method', 'kind']);
  });

  it('tells stream keys apart', () => {
    expect(isStreamKey(['things', 'a', { $stream: true }])).toBe(true);
    expect(isStreamKey(['things', 'a', { $stream: true, delay: 5 }])).toBe(true);
    expect(isStreamKey(['things', 'a'])).toBe(false);
    expect(isStreamKey(['things', { $method: 'HEAD' }])).toBe(false);
    expect(isStreamKey(['things', { $stream: 'yes' }])).toBe(false);
    expect(isStreamKey([])).toBe(false);
  });
});

describe('keyPrefixFor and keyFor', () => {
  it('returns the static segments up to the first parameter', () => {
    expect(keyPrefixFor(getThingSpec)).toEqual(['api', 'things']);
    expect(keyPrefixFor(spec({ path: '/a/b/c' }))).toEqual(['a', 'b', 'c']);
  });

  it('never returns an empty prefix for a route that starts with a parameter', () => {
    const top = spec({ path: '/{id}/items', pathParams: { id: 'id' } });
    expect(keyPrefixFor(top)).toEqual([null, 'items']);
    expect(keyFor(top, z.object({ id: z.string() }))()).toEqual([null, 'items']);
  });

  it('appends the markers to the prefix of a route without parameters, so it is no other exact key', () => {
    const feed = spec({ path: '/feed', queryParams: { kind: 'kind' } });
    const Feed = z.object({ kind: z.string().optional() });
    expect(keyFor(feed, Feed)()).toEqual(['feed']);
    expect(keyFor(feed, Feed, { stream: true })()).toEqual(['feed', { $stream: true }]);
    expect(keyFor({ ...feed, method: 'HEAD' }, Feed, { discriminator: 'method' })()).toEqual(['feed', { $method: 'HEAD' }]);
    // A route with parameters keeps the bare static prefix, which is no request's key.
    expect(keyFor(getThingSpec, GetThingPayload, { stream: true })()).toEqual(['api', 'things']);
  });

  it('returns the full key for a payload, encoded, and the prefix without one', () => {
    const getThingKey = keyFor(getThingSpec, GetThingPayload);
    expect(getThingKey()).toEqual(['api', 'things']);
    expect(getThingKey({ id: 'a', since: new Date('2026-01-02T03:04:05Z') })).toEqual([
      'api',
      'things',
      'a',
      { since: '2026-01-02T03:04:05.000Z' },
    ]);
    const headKey = keyFor({ ...getThingSpec, method: 'HEAD' }, GetThingPayload, { discriminator: 'method' });
    expect(headKey({ id: 'a' })).toEqual(['api', 'things', 'a', { $method: 'HEAD' }]);
    expectTypeOf(getThingKey).parameter(0).toEqualTypeOf<GetThingPayload | undefined>();
  });
});

describe('queryFor', () => {
  it('builds the key and a queryFn that calls the function with the signal and unwraps', async () => {
    const calls: unknown[][] = [];
    const getThing = async (c: Client, payload: GetThingPayload, options?: RequestOptions) => {
      calls.push([c, payload, options]);
      return ok({ name: `thing ${payload.id}` });
    };
    const getThingQuery = queryFor(getThing, getThingSpec, GetThingPayload, { required: true });
    const options = getThingQuery(client, { id: 'a', tags: ['x'] });
    expect(options.queryKey).toEqual(['api', 'things', 'a', { tags: ['x'] }]);
    expect(Object.keys(options).sort()).toEqual(['queryFn', 'queryKey']);
    const signal = new AbortController().signal;
    await expect(options.queryFn({ signal })).resolves.toEqual({ name: 'thing a' });
    expect(calls).toEqual([[client, { id: 'a', tags: ['x'] }, { signal }]]);
    expectTypeOf(options.queryFn).returns.toEqualTypeOf<Promise<{ name: string }>>();
  });

  it('never puts credentials or the base URL in the key', () => {
    const getThingQuery = queryFor(async () => ok(1), getThingSpec, GetThingPayload, { required: true });
    const text = JSON.stringify(getThingQuery(client, { id: 'a' }).queryKey);
    expect(text).not.toContain('secret-token');
    expect(text).not.toContain('toy.test');
    expect(text).not.toContain('base');
  });

  it('rejects in queryFn when the payload is invalid, under a key no real request has', async () => {
    const getThingQuery = queryFor(async () => ok(1), getThingSpec, GetThingPayload, { required: true });
    const options = getThingQuery(client, { id: 7 as unknown as string });
    expect(options.queryKey).toEqual(['api', 'things', null, { $invalid: true }]);
    // Not the key of the list under the same prefix, whose cache entry it would otherwise take over.
    const listSpec = spec({ path: '/api/things', queryParams: { kind: 'kind' } });
    const listQuery = queryFor(
      async (_c: Client, _p?: { kind?: string }) => ok([]),
      listSpec,
      z.object({ kind: z.string().optional() }),
      {},
    );
    expect(options.queryKey).not.toEqual(listQuery(client).queryKey);
    expect(listQuery(client).queryKey).toEqual(['api', 'things']);
    await expect(options.queryFn({ signal: new AbortController().signal })).rejects.toBeInstanceOf(RequestValidationError);
  });

  it('disables a query whose required payload is undefined (dependent query)', async () => {
    let called = false;
    const getThingQuery = queryFor(
      async (_c: Client, _p: GetThingPayload) => {
        called = true;
        return ok(1);
      },
      getThingSpec,
      GetThingPayload,
      { required: true },
    );
    const options = getThingQuery(client, undefined);
    expect(options.enabled).toBe(false);
    // Never the prefix, which another method's key may equal.
    expect(options.queryKey).toEqual(['api', 'things', null, { $disabled: true }]);
    await expect(options.queryFn({ signal: new AbortController().signal })).rejects.toThrow(
      'query disabled: payload is undefined',
    );
    expect(called).toBe(false);
    // The payload must be passed, if only as undefined.
    // @ts-expect-error missing payload
    getThingQuery(client);
  });

  it('disables a query without path parameters under a key that is not the list key', () => {
    const Required = z.object({ kind: z.string() });
    const listSpec = spec({ queryParams: { kind: 'kind' } });
    const byKind = queryFor(async (_c: Client, _p: z.output<typeof Required>) => ok([]), listSpec, Required, {
      required: true,
    });
    const list = queryFor(async (_c: Client, _p?: undefined) => ok([]), spec({}), null, {});
    expect(list(client).queryKey).toEqual(['things']);
    expect(byKind(client, undefined).queryKey).toEqual(['things', { $disabled: true }]);
    expect(byKind(client, undefined).queryKey).not.toEqual(list(client).queryKey);
  });

  it('treats an omitted optional payload as empty, and a missing payload as none', async () => {
    const List = z.object({ kind: z.string().optional() });
    const list = async (_c: Client, payload?: z.output<typeof List>) => ok(payload ?? null);
    const listQuery = queryFor(list, spec({ queryParams: { kind: 'kind' } }), List, {});
    expect(listQuery(client).queryKey).toEqual(['things']);
    expect(listQuery(client).enabled).toBeUndefined();
    expect(listQuery(client, { kind: 'a' }).queryKey).toEqual(['things', { kind: 'a' }]);
    await expect(listQuery(client).queryFn({ signal: new AbortController().signal })).resolves.toBeNull();

    const none = async (_c: Client, _p?: undefined) => ok('x');
    const noneQuery = queryFor(none, spec({ path: '/ping' }), null, {});
    expect(noneQuery(client).queryKey).toEqual(['ping']);
  });

  it('resolves a void read to null, after the call', async () => {
    let called = false;
    const ping = async (_c: Client, _p?: undefined): Promise<CallResult<void, never>> => {
      called = true;
      return ok(undefined);
    };
    const pingQuery = queryFor(ping, spec({ path: '/ping' }), null, { void: true });
    const options = pingQuery(client);
    expectTypeOf(options.queryFn).returns.toEqualTypeOf<Promise<null>>();
    await expect(options.queryFn({ signal: new AbortController().signal })).resolves.toBeNull();
    expect(called).toBe(true);
    const fail = queryFor(
      async (_c: Client, _p?: undefined): Promise<CallResult<void, RequestValidationError>> => ({
        ok: false,
        error: new RequestValidationError([]),
      }),
      spec({ path: '/ping' }),
      null,
      { void: true },
    );
    await expect(fail(client).queryFn({ signal: new AbortController().signal })).rejects.toBeInstanceOf(RequestValidationError);
  });

  it('discriminates the key when asked', () => {
    const headQuery = queryFor(async () => ok(1), { ...getThingSpec, method: 'HEAD' }, GetThingPayload, {
      required: true,
      discriminator: 'method',
    });
    expect(headQuery(client, { id: 'a' }).queryKey).toEqual(['api', 'things', 'a', { $method: 'HEAD' }]);
    expect(headQuery(client, undefined).queryKey).toEqual(['api', 'things', null, { $method: 'HEAD', $disabled: true }]);
    expect(headQuery(client, { id: 1 as unknown as string }).queryKey).toEqual([
      'api',
      'things',
      null,
      { $method: 'HEAD', $invalid: true },
    ]);
  });
});

describe('streamFor', () => {
  type Event = { n: number };
  async function* events(): AsyncGenerator<Event> {
    yield { n: 1 };
    yield { n: 2 };
  }
  const watchSpec = spec({ path: '/things/{id}/events', pathParams: { id: 'id' }, queryParams: { delay: 'delay_ms' } });
  const Watch = z.object({ id: z.string(), delay: z.number().optional() });
  const watch = async (_c: Client, _p: z.output<typeof Watch>, options?: { signal?: AbortSignal | undefined }) => {
    seen.push(options?.signal);
    return ok(events() as AsyncIterable<Event>);
  };
  const seen: (AbortSignal | undefined)[] = [];

  /** fake records what a streamedQuery receives and returns a marker. */
  function fake() {
    const received: StreamedQueryParams<Event, unknown>[] = [];
    const streamedQuery = <TData>(params: StreamedQueryParams<Event, TData>) => {
      received.push(params as StreamedQueryParams<Event, unknown>);
      return (_context: { signal: AbortSignal }) => Promise.resolve('marker' as unknown as TData);
    };
    return { received, streamedQuery };
  }

  it('passes a streamFn and the pass-through fields to streamedQuery, with staleTime Infinity', async () => {
    const watchQuery = streamFor(watch, watchSpec, Watch, { required: true });
    const { received, streamedQuery } = fake();
    const reducer = (acc: number, e: Event) => acc + e.n;
    const options = watchQuery(client, { id: 'a', delay: 5 }, { streamedQuery, refetchMode: 'append', reducer, initialValue: 0 });
    expect(options.queryKey).toEqual(['things', 'a', 'events', { $stream: true, delay: 5 }]);
    expect(options.staleTime).toBe(Infinity);
    expect(options.enabled).toBeUndefined();
    expect(received).toHaveLength(1);
    const params = received[0]!;
    expect(params.refetchMode).toBe('append');
    expect(params.reducer).toBe(reducer);
    expect(params.initialValue).toBe(0);
    expect(Object.keys(params).sort()).toEqual(['initialValue', 'reducer', 'refetchMode', 'streamFn']);
    await expect(options.queryFn({ signal: new AbortController().signal })).resolves.toBe('marker');

    const signal = new AbortController().signal;
    const stream = await params.streamFn({ signal });
    const got: Event[] = [];
    for await (const e of stream) got.push(e);
    expect(got).toEqual([{ n: 1 }, { n: 2 }]);
    expect(seen.at(-1)).toBe(signal);
  });

  it('passes nothing it was not given', () => {
    const watchQuery = streamFor(watch, watchSpec, Watch, { required: true });
    const { received, streamedQuery } = fake();
    watchQuery(client, { id: 'a' }, { streamedQuery });
    expect(Object.keys(received[0]!)).toEqual(['streamFn']);
  });

  it('types the reducer event as the stream event', () => {
    const watchQuery = streamFor(watch, watchSpec, Watch, { required: true });
    const { streamedQuery } = fake();
    watchQuery(
      client,
      { id: 'a' },
      {
        streamedQuery,
        reducer: (acc: string, e) => {
          expectTypeOf(e).toEqualTypeOf<Event>();
          return acc + e.n;
        },
        initialValue: '',
      },
    );
  });

  it('disables a stream whose required payload is undefined', async () => {
    const watchQuery = streamFor(watch, watchSpec, Watch, { required: true });
    const { received, streamedQuery } = fake();
    const options = watchQuery(client, undefined, { streamedQuery });
    expect(options.enabled).toBe(false);
    expect(options.staleTime).toBe(Infinity);
    expect(options.queryKey).toEqual(['things', null, 'events', { $stream: true, $disabled: true }]);
    await expect(received[0]!.streamFn({ signal: new AbortController().signal })).rejects.toThrow(
      'query disabled: payload is undefined',
    );
  });
});

describe('invalidatesFor and mutationFor', () => {
  const labelSpec = spec({
    method: 'DELETE',
    path: '/things/{id}/labels/{lid}',
    pathParams: { id: 'id', label: 'lid' },
  });
  const Label = z.object({ id: z.string(), label: z.string() });

  function fakeQueryClient() {
    const invalidated: QueryKey[] = [];
    const predicates: ((query: { queryKey: QueryKey }) => boolean)[] = [];
    return {
      invalidated,
      predicates,
      invalidateQueries: async (filters: InvalidateFilters) => {
        invalidated.push(filters.queryKey);
        if (filters.predicate) predicates.push(filters.predicate);
      },
    };
  }

  it('substitutes path parameters from the variables, stopping where they run out', () => {
    const invalidates = invalidatesFor(labelSpec, Label, ['/things/{id}/labels', '/other']);
    expect(invalidates({ id: 'a', label: 'l' })).toEqual([['things', 'a', 'labels'], ['other']]);
    expect(invalidates()).toEqual([['things'], ['other']]);
  });

  it('invalidates every key on success when given a queryClient, awaiting them all', async () => {
    const invalidates = invalidatesFor(labelSpec, Label, ['/things/{id}/labels', '/things']);
    const del = async (_c: Client, payload: z.output<typeof Label>) => ok(payload.label);
    const delMutation = mutationFor(del, invalidates);
    const qc = fakeQueryClient();
    const options = delMutation(client, { queryClient: qc });
    await expect(options.mutationFn({ id: 'a', label: 'l' })).resolves.toBe('l');
    await options.onSuccess!('l', { id: 'a', label: 'l' });
    expect(qc.invalidated).toEqual([['things', 'a', 'labels'], ['things']]);
  });

  it('never invalidates a stream key', async () => {
    const invalidates = invalidatesFor(labelSpec, Label, ['/things']);
    const qc = fakeQueryClient();
    const options = mutationFor(async (_c: Client, _p: z.output<typeof Label>) => ok(1), invalidates)(client, {
      queryClient: qc,
    });
    await options.onSuccess!(1, { id: 'a', label: 'l' });
    expect(qc.predicates).toHaveLength(1);
    const keep = qc.predicates[0]!;
    expect(keep({ queryKey: ['things', 'a', { verbose: true }] })).toBe(true);
    expect(keep({ queryKey: ['things'] })).toBe(true);
    expect(keep({ queryKey: ['things', 'a', 'events', { $stream: true }] })).toBe(false);
  });

  it('drops a key that would be empty', () => {
    const top = spec({ method: 'DELETE', path: '/{id}', pathParams: { id: 'id' } });
    const invalidates = invalidatesFor(top, z.object({ id: z.string() }), ['/{id}']);
    expect(invalidates({ id: 'a' })).toEqual([['a']]);
    expect(invalidates()).toEqual([]);
  });

  it('omits onSuccess without a queryClient', () => {
    const delMutation = mutationFor(async (_c: Client, _p?: undefined) => ok(undefined), invalidatesFor(labelSpec, null, []));
    const options = delMutation(client);
    expect(Object.keys(options)).toEqual(['mutationFn']);
    // A mutation without a payload is called without variables.
    void options.mutationFn();
  });

  it('throws the error of a failed call from mutationFn', async () => {
    const failure = new RequestValidationError([]);
    const fail = async (_c: Client, _p: z.output<typeof Label>): Promise<CallResult<number, RequestValidationError>> => ({
      ok: false,
      error: failure,
    });
    const options = mutationFor(fail, invalidatesFor(labelSpec, Label, []))(client);
    await expect(options.mutationFn({ id: 'a', label: 'l' })).rejects.toBe(failure);
  });

  it('takes { payload, body } for a raw upload', async () => {
    const uploadSpec = spec({ method: 'PUT', path: '/things/{id}/content', pathParams: { id: 'id' }, body: { kind: 'raw' } });
    const Upload = z.object({ id: z.string() });
    const sent: unknown[] = [];
    const upload = async (_c: Client, payload: z.output<typeof Upload>, body: BodyInit, _o?: RequestOptions) => {
      sent.push(payload, body);
      return ok({ size: 2 });
    };
    const invalidates = invalidatesFor(uploadSpec, Upload, ['/things/{id}/content'], { raw: true });
    const uploadMutation = mutationFor(upload, invalidates, { raw: true });
    const qc = fakeQueryClient();
    const options = uploadMutation(client, { queryClient: qc });
    const variables = { payload: { id: 'a' }, body: 'hi' };
    await expect(options.mutationFn(variables)).resolves.toEqual({ size: 2 });
    expect(sent).toEqual([{ id: 'a' }, 'hi']);
    await options.onSuccess!({ size: 2 }, variables);
    expect(qc.invalidated).toEqual([['things', 'a', 'content']]);
    expectTypeOf(options.mutationFn).parameter(0).toEqualTypeOf<{ payload: { id: string }; body: BodyInit }>();
  });
});
