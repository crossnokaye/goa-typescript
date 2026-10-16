/**
 * Code that is generic over endpoints: one call shape for every function and
 * one error type every union narrows to (ADR 0003).
 */
import {
  type CallResult,
  type Client,
  type ClientError,
  type EndpointError,
  type GoaError,
  type Middleware,
  type RequestOptions,
  type ThingReply,
  type ThingWithViews,
  toy,
  unwrap,
  unwrapAsync,
} from 'toy-client';
import { describe, expect, expectTypeOf, it } from 'vitest';

import { client, collect } from './helpers.js';

/** spy records every request the client sends. */
function spy(): { middleware: Middleware; requests: Request[] } {
  const requests: Request[] = [];
  return {
    requests,
    middleware: (req, next) => {
      requests.push(req.clone());
      return next(req);
    },
  };
}

/** Endpoint is the shape of every generated function, as a wrapper sees it. */
type Endpoint<Req, Res> = (c: Client, payload: Req, o?: RequestOptions) => Promise<CallResult<Res, EndpointError>>;

/** call is a thin wrapper that adds nothing but a type: it compiles for any function. */
function call<Req, Res>(fn: Endpoint<Req, Res>, c: Client, payload: Req, o?: RequestOptions): Promise<Res> {
  return unwrapAsync(fn(c, payload, o));
}

describe('EndpointError', () => {
  it("is what every function's error union narrows to", () => {
    expectTypeOf<toy.GetThingError>().toExtend<EndpointError>();
    expectTypeOf<toy.CreateThingError>().toExtend<EndpointError>();
    expectTypeOf<toy.ListThingsError>().toExtend<EndpointError>();
    expectTypeOf<toy.StreamThingsSseError>().toExtend<EndpointError>();
    expectTypeOf<ClientError>().toExtend<EndpointError>();
    expectTypeOf<GoaError<'not_found'>>().toExtend<EndpointError>();
    expectTypeOf<Error>().not.toExtend<EndpointError>();
  });

  it('types a formatter shared by every endpoint', async () => {
    const seen: string[] = [];
    const report = (e: EndpointError) => seen.push(e.kind === 'goa' ? `${e.name}:${e.status}` : e.kind);
    const missing = await toy.getThing(client(), { id: 'missing' });
    if (!missing.ok) report(missing.error);
    const invalid = await toy.createThing(client(), { name: 'bad' });
    if (!invalid.ok) report(invalid.error);
    const unreachable = await toy.listThings(client({ baseURL: 'http://127.0.0.1:1' }));
    if (!unreachable.ok) report(unreachable.error);
    expect(seen).toEqual(['not_found:404', 'invalid:422', 'network']);
  });
});

describe('one call shape', () => {
  it('takes the payload second whatever the method declares', () => {
    expectTypeOf(toy.getThing).parameter(1).toEqualTypeOf<toy.GetThingPayload>();
    expectTypeOf(toy.getThingViewed).parameter(1).toEqualTypeOf<toy.GetThingViewedPayload | undefined>();
    expectTypeOf(toy.listThings).parameter(1).toEqualTypeOf<undefined>();
    expectTypeOf(toy.streamCountSse).parameter(1).toEqualTypeOf<undefined>();
    expectTypeOf(toy.streamViewedSse).parameter(1).toEqualTypeOf<toy.StreamViewedSsePayload | undefined>();
    expectTypeOf(toy.listThings).parameters.toEqualTypeOf<
      [client: Client, payload?: undefined, options?: RequestOptions]
    >();
    // @ts-expect-error the options are not a payload
    void toy.listThings(client(), { signal: AbortSignal.timeout(1) });
    // @ts-expect-error a required attribute cannot be omitted
    void toy.getThing(client());
  });

  it('lets a payload-less function be called with the client alone, or with options', async () => {
    const seen = spy();
    const things = await unwrapAsync(toy.listThings(client({ middleware: [seen.middleware] })));
    expect(things).toHaveLength(2);
    const signal = AbortSignal.timeout(5_000);
    const again = await toy.listThings(client({ middleware: [seen.middleware] }), undefined, { signal });
    expect(unwrap(again)).toEqual(things);
    expect(seen.requests.map((r) => r.url.endsWith('/things'))).toEqual([true, true]);
  });

  it('lets an all-optional payload be omitted', async () => {
    const seen = spy();
    const viewed = await unwrapAsync(toy.getThingViewed(client({ middleware: [seen.middleware] })));
    expect(viewed).toEqual({ id: 'viewed', name: 'viewed-thing' });
    expect(unwrap(await toy.getThingViewed(client(), {}))).toEqual(viewed);
    // The omitted payload encodes as {} and the server applies its defaults.
    expect(new URL(seen.requests[0]!.url).search).toBe('');
    const events = await collect(await unwrapAsync(toy.streamViewedSse(client())));
    expect(events).toHaveLength(1);
  });

  it('fits one wrapper type for a payload-less, an optional-payload and a payload function', async () => {
    const c = client();
    const things = await call(toy.listThings, c, undefined);
    expect(things).toHaveLength(2);
    const viewed = await call(toy.getThingViewed, c, undefined, { signal: AbortSignal.timeout(5_000) });
    expect(viewed.id).toBe('viewed');
    const one = await call(toy.getThing, c, { id: 'abc' });
    expect(one.thing.name).toBe('full-thing');
    // An SSE function has the same shape, with options that extend RequestOptions.
    const stream = await call(toy.streamCountSse, c, undefined);
    expect(await collect(stream)).toEqual([1, 2, 3]);
    // The wrapper's types come from the function, not from the call.
    expectTypeOf(call(toy.getThing, c, { id: 'abc' })).resolves.toEqualTypeOf<ThingReply>();
    expectTypeOf(call(toy.listThings, c, undefined)).resolves.toEqualTypeOf<ThingWithViews[]>();
  });

  it('keeps the raw-body shape, with the body after the payload', async () => {
    expectTypeOf(toy.uploadThing).parameters.toEqualTypeOf<
      [client: Client, payload: toy.UploadThingPayload, body: BodyInit, options?: RequestOptions]
    >();
    const uploaded = await unwrapAsync(toy.uploadThing(client(), { id: 'abc' }, 'bytes'));
    expect(uploaded.size).toBe(5);
  });
});
