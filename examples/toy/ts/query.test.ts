import {
  experimental_streamedQuery as streamedQuery,
  type MutationOptions,
  MutationObserver,
  QueryClient,
  QueryObserver,
  type QueryObserverOptions,
} from '@tanstack/query-core';
import {
  type EndpointError,
  type Middleware,
  RequestValidationError,
  type Thing,
  type ThingEvent,
  type ThingReply,
  type ThingSummary,
  type Tick,
  toy,
} from 'toy-client';
import { afterEach, describe, expect, expectTypeOf, it } from 'vitest';

import { client, token } from './helpers.js';

// The generated query options driven by a real TanStack QueryClient, without
// React: what useQuery and useMutation would do with them.

/** spy records every request the client sends. */
function spy(): { middleware: Middleware; requests: Request[] } {
  const requests: Request[] = [];
  return {
    requests,
    middleware: (req, next) => {
      requests.push(req);
      return next(req);
    },
  };
}

const clients: QueryClient[] = [];

/** queryClient returns a QueryClient that does not retry, cleared after each test. */
function queryClient(): QueryClient {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(qc);
  return qc;
}

afterEach(() => {
  for (const qc of clients.splice(0)) {
    qc.clear();
  }
});

/** until polls a condition, for state a stream fills in over time. */
async function until(condition: () => boolean, ms = 5_000): Promise<void> {
  const deadline = Date.now() + ms;
  while (!condition()) {
    if (Date.now() > deadline) {
      throw new Error('condition not met in time');
    }
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
}

describe('read queries', () => {
  it('fetchQuery returns the decoded value under the documented key', async () => {
    const qc = queryClient();
    const options = toy.getThingQuery(client(), { id: 'abc', tags: ['x', 'y'], verbose: true, trace: 't1' });
    expect(options.queryKey).toEqual(['things', 'abc', { tags: ['x', 'y'], verbose: true, trace: 't1' }]);
    // The credential and the base URL never enter the key.
    expect(JSON.stringify(options.queryKey)).not.toContain(token);
    expect(JSON.stringify(options.queryKey)).not.toContain('127.0.0.1');

    const reply = await qc.fetchQuery(options);
    expectTypeOf(reply).toEqualTypeOf<ThingReply>();
    expect(reply.thing.name).toBe('full-thing');
    expect(reply.thing.created_at).toBeInstanceOf(Date);
    expect(reply.tags).toEqual(['x', 'y']);
    expect(qc.getQueryData(options.queryKey)).toBe(reply);
    expect(toy.getThingKey({ id: 'abc', tags: ['x', 'y'], verbose: true, trace: 't1' })).toEqual(options.queryKey);
    expect(toy.getThingKey()).toEqual(['things']);
  });

  it('keys a date-time in its wire form', () => {
    const since = new Date('2026-01-02T03:04:05Z');
    expect(toy.getThingQuery(client(), { id: 'abc', since }).queryKey).toEqual([
      'things',
      'abc',
      { since: '2026-01-02T03:04:05.000Z' },
    ]);
  });

  it('rejects with the typed goa error', async () => {
    const qc = queryClient();
    const error = await qc.fetchQuery(toy.getThingQuery(client(), { id: 'missing' })).catch((e: unknown) => e);
    expect(error).toMatchObject({ kind: 'goa', name: 'not_found', status: 404 });
  });

  it('disables a dependent query whose payload is undefined, and sends nothing', async () => {
    const qc = queryClient();
    const rec = spy();
    const options = toy.getThingQuery(client({ middleware: [rec.middleware] }), undefined);
    expect(options.enabled).toBe(false);
    const observer = new QueryObserver(qc, options);
    const unsubscribe = observer.subscribe(() => {});
    await new Promise((resolve) => setTimeout(resolve, 50));
    const result = observer.getCurrentResult();
    expect(result.status).toBe('pending');
    expect(result.fetchStatus).toBe('idle');
    expect(rec.requests).toHaveLength(0);
    unsubscribe();
  });

  it('runs a POST read, keyed by its body in wire form', async () => {
    const qc = queryClient();
    const id = '44444444-4444-4444-8444-444444444444';
    const created_at = new Date('2026-01-02T03:04:05Z');
    const options = toy.echoThingQuery(client(), { thing: { id, name: 'echoed', tags: ['t'], created_at } });
    expect(options.queryKey).toEqual([
      'things',
      'echo',
      { thing: { id, name: 'echoed', tags: ['t'], created_at: '2026-01-02T03:04:05.000Z' } },
    ]);
    const thing = await qc.fetchQuery(options);
    expectTypeOf(thing).toEqualTypeOf<Thing>();
    expect(thing).toMatchObject({ id, name: 'echoed', kind: 'widget', enabled: true, created_at });
  });

  it('keys an invalid payload apart from every real request and rejects with the validation error', async () => {
    const qc = queryClient();
    const options = toy.echoThingQuery(client(), { thing: { id: 'not-a-uuid' } });
    expect(options.queryKey).toEqual(['things', 'echo', { $invalid: true }]);
    await expect(qc.fetchQuery(options)).rejects.toBeInstanceOf(RequestValidationError);
  });

  it('leaves the list alone when a read under its prefix has an invalid payload', async () => {
    const qc = queryClient();
    const list = await qc.fetchQuery(toy.listThingsQuery(client()));
    const invalid = toy.getThingQuery(client(), { id: 'abc', since: new Date(Number.NaN) });
    expect(invalid.queryKey).toEqual(['things', null, { $invalid: true }]);
    expect(invalid.queryKey).not.toEqual(toy.listThingsKey());
    await expect(qc.fetchQuery(invalid)).rejects.toBeInstanceOf(RequestValidationError);
    expect(qc.getQueryState(toy.listThingsKey())?.status).toBe('success');
    expect(qc.getQueryData(toy.listThingsKey())).toBe(list);
  });

  it('keeps a HEAD apart from the GET on the same route', async () => {
    const qc = queryClient();
    const head = toy.headThingQuery(client(), { thing_id: 'abc' });
    const get = toy.getThingQuery(client(), { id: 'abc' });
    expect(head.queryKey).toEqual(['things', 'abc', { $method: 'HEAD' }]);
    expect(get.queryKey).toEqual(['things', 'abc']);
    const [etag, reply] = await Promise.all([qc.fetchQuery(head), qc.fetchQuery(get)]);
    expect(etag).toEqual({ etag: '"abc-v1"' });
    expect(reply.thing.name).toBe('full-thing');
    expect(qc.getQueryData(head.queryKey)).toEqual({ etag: '"abc-v1"' });
    expect(qc.getQueryData(get.queryKey)).toBe(reply);
  });

  it('keeps the plain function and the stream of a mixed method apart', async () => {
    const qc = queryClient();
    const plain = toy.streamOrListQuery(client());
    const streamed = toy.streamOrListStreamQuery(client(), undefined, { streamedQuery });
    expect(plain.queryKey).toEqual(['stream-or-list']);
    expect(streamed.queryKey).toEqual(['stream-or-list', { $stream: true }]);
    const [list, events] = await Promise.all([qc.fetchQuery(plain), qc.fetchQuery(streamed)]);
    expectTypeOf(list).toEqualTypeOf<ThingSummary[]>();
    expect(list.map((t) => t.id)).toEqual(['one', 'two']);
    expect(events.map((t) => t.id)).toEqual(['one', 'two']);
  });

  it('writes through a stream key without a payload without touching the plain entry', async () => {
    const qc = queryClient();
    const plain = toy.streamOrListQuery(client());
    const list = await qc.fetchQuery(plain);
    const prefix = toy.streamOrListStreamKey();
    expect(prefix).toEqual(['stream-or-list', { $stream: true }]);
    expect(prefix).not.toEqual(toy.streamOrListKey());
    qc.setQueryData(prefix, [{ id: 'written' }]);
    expect(qc.getQueryData(plain.queryKey)).toBe(list);
    expect(qc.getQueryData(prefix)).toEqual([{ id: 'written' }]);
  });

  it('resolves a read without a result to null', async () => {
    const qc = queryClient();
    const options = toy.pingQuery(client());
    expect(options.queryKey).toEqual(['ping']);
    const data = await qc.fetchQuery(options);
    expectTypeOf(data).toEqualTypeOf<null>();
    expect(data).toBeNull();
    expect(qc.getQueryState(options.queryKey)?.status).toBe('success');
  });
});

describe('stream queries', () => {
  it('accumulates the events of a Server-Sent Events stream through the real streamedQuery', async () => {
    const qc = queryClient();
    const options = toy.streamThingsSseQuery(client(), { id: 'abc' }, { streamedQuery });
    expect(options.queryKey).toEqual(['things', 'abc', 'events', { $stream: true }]);
    expect(options.staleTime).toBe(Infinity);
    const events = await qc.fetchQuery(options);
    expectTypeOf(events).toEqualTypeOf<ThingEvent[]>();
    expect(events.map((e) => e.id)).toEqual(['abc-1', 'abc-2', 'abc-3']);
  });

  it('closes the connection when the query is cancelled', async () => {
    const qc = queryClient();
    const rec = spy();
    const options = toy.streamThingsSseQuery(
      client({ middleware: [rec.middleware] }),
      { id: 'slow', delay_ms: 200 },
      { streamedQuery },
    );
    const observer = new QueryObserver(qc, options);
    const unsubscribe = observer.subscribe(() => {});
    await until(() => (qc.getQueryData<ThingEvent[]>(options.queryKey) ?? []).length >= 1);
    expect(rec.requests).toHaveLength(1);
    expect(rec.requests[0]!.signal.aborted).toBe(false);

    await qc.cancelQueries({ queryKey: options.queryKey });
    expect(rec.requests[0]!.signal.aborted).toBe(true);
    expect(qc.getQueryState(options.queryKey)?.fetchStatus).toBe('idle');
    unsubscribe();
  });

  it('folds the messages of a WebSocket stream with a reducer, and closes the socket on cancel', async () => {
    const qc = queryClient();
    const sum = await qc.fetchQuery(
      toy.watchTicksQuery(
        client(),
        { id: 'abc' },
        { streamedQuery, reducer: (acc, tick) => acc + tick.n, initialValue: 0 },
      ),
    );
    expectTypeOf(sum).toEqualTypeOf<number>();
    expect(sum).toBe(6);

    const sockets: WebSocket[] = [];
    const options = toy.watchTicksQuery(
      client({
        webSocket: (url, protocols) => {
          const socket = new WebSocket(url, protocols);
          sockets.push(socket);
          return socket;
        },
      }),
      { id: 'slow', count: 100, delay_ms: 20 },
      { streamedQuery },
    );
    const observer = new QueryObserver(qc, options);
    const unsubscribe = observer.subscribe(() => {});
    await until(() => (qc.getQueryData<Tick[]>(options.queryKey) ?? []).length >= 1);
    await qc.cancelQueries({ queryKey: options.queryKey });
    await until(() => sockets[0]!.readyState === WebSocket.CLOSED);
    unsubscribe();
  });

  it('disables a stream whose required payload is undefined', () => {
    const options = toy.streamThingsSseQuery(client(), undefined, { streamedQuery });
    expect(options.enabled).toBe(false);
    expect(options.queryKey).toEqual(['things', null, 'events', { $stream: true, $disabled: true }]);
  });
});

describe('mutations', () => {
  it('invalidates the list key with the real QueryClient', async () => {
    const qc = queryClient();
    await qc.fetchQuery(toy.listThingsQuery(client()));
    await qc.fetchQuery(toy.getThingQuery(client(), { id: 'abc' }));
    await qc.fetchQuery(toy.streamOrListQuery(client()));
    expect(qc.getQueryState(toy.listThingsKey())?.isInvalidated).toBe(false);

    const options = toy.createThingMutation(client(), { queryClient: qc });
    const thing = await new MutationObserver(qc, options).mutate({ name: 'new-thing' });
    expectTypeOf(thing).toEqualTypeOf<Thing>();
    expect(thing.name).toBe('new-thing');

    // POST /things invalidates ["things"]: the list and every thing under it.
    expect(toy.createThingInvalidates({ name: 'x' })).toEqual([['things']]);
    expect(qc.getQueryState(['things'])?.isInvalidated).toBe(true);
    expect(qc.getQueryState(toy.getThingKey({ id: 'abc' }))?.isInvalidated).toBe(true);
    expect(qc.getQueryState(['stream-or-list'])?.isInvalidated).toBe(false);
  });

  it('refetches an observed query it invalidates', async () => {
    const qc = queryClient();
    const rec = spy();
    const observer = new QueryObserver(qc, toy.listThingsQuery(client({ middleware: [rec.middleware] })));
    const unsubscribe = observer.subscribe(() => {});
    await until(() => observer.getCurrentResult().status === 'success');
    expect(rec.requests).toHaveLength(1);
    await new MutationObserver(qc, toy.deleteThingMutation(client(), { queryClient: qc })).mutate({ id: 'abc' });
    await until(() => rec.requests.length === 2);
    unsubscribe();
  });

  it('leaves invalidation out without a queryClient', () => {
    expect(Object.keys(toy.upsertThingMutation(client()))).toEqual(['mutationFn']);
  });

  it('substitutes path parameters from the variables of a raw upload, up to a key a read lives under', async () => {
    const qc = queryClient();
    await qc.fetchQuery(toy.getThingQuery(client(), { id: 'up' }));
    await qc.fetchQuery(toy.getThingQuery(client(), { id: 'other' }));
    const options = toy.uploadThingMutation(client(), { queryClient: qc });
    const variables = { payload: { id: 'up', content_type: 'text/plain' }, body: 'hello' };
    const result = await new MutationObserver(qc, options).mutate(variables);
    expect(result).toMatchObject({ size: 5, text: 'hello' });
    // PUT /things/{id}/content: no query lives under its own path, so it invalidates the thing's.
    expect(toy.uploadThingInvalidates(variables)).toEqual([['things', 'up']]);
    expect(qc.getQueryState(toy.getThingKey({ id: 'up' }))?.isInvalidated).toBe(true);
    expect(qc.getQueryState(toy.getThingKey({ id: 'other' }))?.isInvalidated).toBe(false);
  });

  it('never restarts an open stream', async () => {
    const qc = queryClient();
    const rec = spy();
    const options = toy.streamThingsSseQuery(
      client({ middleware: [rec.middleware] }),
      { id: 'slow', delay_ms: 200 },
      {
        streamedQuery,
      },
    );
    const observer = new QueryObserver(qc, options);
    const unsubscribe = observer.subscribe(() => {});
    await until(() => (qc.getQueryData<ThingEvent[]>(options.queryKey) ?? []).length >= 1);

    await new MutationObserver(qc, toy.upsertThingMutation(client(), { queryClient: qc })).mutate({
      id: 'other',
      name: 'renamed',
    });
    expect(qc.getQueryState(options.queryKey)?.isInvalidated).toBe(false);
    expect(rec.requests).toHaveLength(1);
    expect(rec.requests[0]!.signal.aborted).toBe(false);
    unsubscribe();
  });

  it('rejects with the typed goa error', async () => {
    const qc = queryClient();
    const error = await new MutationObserver(qc, toy.deleteThingMutation(client()))
      .mutate({ id: 'missing' })
      .catch((e: unknown) => e);
    expect(error).toMatchObject({ kind: 'goa', name: 'not_found' });
  });
});

describe('types against @tanstack/query-core', () => {
  it('are assignable to the options TanStack declares', () => {
    const read: QueryObserverOptions<ThingReply, EndpointError, ThingReply, ThingReply, readonly unknown[]> =
      toy.getThingQuery(client(), { id: 'abc' });
    const disabled: QueryObserverOptions<ThingReply, EndpointError, ThingReply, ThingReply, readonly unknown[]> =
      toy.getThingQuery(client(), undefined);
    const none: QueryObserverOptions<ThingSummary[], Error, ThingSummary[], ThingSummary[], readonly unknown[]> =
      toy.streamOrListQuery(client());
    const stream: QueryObserverOptions<ThingEvent[], Error, ThingEvent[], ThingEvent[], readonly unknown[]> =
      toy.streamThingsSseQuery(client(), { id: 'abc' }, { streamedQuery });
    const folded: QueryObserverOptions<number, Error, number, number, readonly unknown[]> = toy.watchTicksQuery(
      client(),
      { id: 'abc' },
      { streamedQuery, reducer: (acc, tick) => acc + tick.n, initialValue: 0 },
    );
    const mutation: MutationOptions<Thing, Error, toy.CreateThingPayload> = toy.createThingMutation(client(), {
      queryClient: new QueryClient(),
    });
    const noResult: MutationOptions<void, Error, toy.DeleteThingPayload> = toy.deleteThingMutation(client());
    const upload: MutationOptions<toy.UploadThingResult, Error, { payload: toy.UploadThingPayload; body: BodyInit }> =
      toy.uploadThingMutation(client());
    void [read, disabled, none, stream, folded, mutation, noResult, upload];

    // @ts-expect-error a reducer needs an initialValue
    toy.watchTicksQuery(client(), { id: 'abc' }, { streamedQuery, reducer: (acc: number, tick) => acc + tick.n });
    // @ts-expect-error the stream's data is the events, not a number
    const wrong: QueryObserverOptions<number, Error, number, number, readonly unknown[]> = toy.watchTicksQuery(
      client(),
      { id: 'abc' },
      { streamedQuery },
    );
    void wrong;
    // @ts-expect-error a required payload must be passed, if only as undefined
    toy.getThingQuery(client());
  });
});
