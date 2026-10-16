import {
  createClient,
  echo,
  isGoaError,
  type Middleware,
  NetworkError,
  type Thing,
  type ThingEvent,
  toy,
  UnexpectedResponseError,
  unwrapAsync,
} from 'toy-client';
import * as echoModule from 'toy-client/echo';
import { ThingEvent as ThingEventSchema } from 'toy-client/types';
import { describe, expect, expectTypeOf, it } from 'vitest';

import { baseURL, client, collect, token } from './helpers.js';

/** spy records the Authorization header and URL of every request. */
function spy(): { middleware: Middleware; authorization: (string | null)[]; urls: string[] } {
  const authorization: (string | null)[] = [];
  const urls: string[] = [];
  return {
    authorization,
    urls,
    middleware: (req, next) => {
      authorization.push(req.headers.get('authorization'));
      urls.push(req.url);
      return next(req);
    },
  };
}

describe('JWT security', () => {
  it('keeps the token out of the request type', () => {
    expectTypeOf<toy.StreamThingsSsePayload>().toEqualTypeOf<{ id: string; delay_ms?: number | undefined }>();
    expectTypeOf(toy.streamTextSse).parameters.toEqualTypeOf<
      [client: ReturnType<typeof createClient>, payload?: undefined, options?: Parameters<typeof toy.streamTextSse>[2]]
    >();
  });

  it('sends the token as a bearer credential', async () => {
    const seen = spy();
    await unwrapAsync(toy.streamCountSse(client({ middleware: [seen.middleware] }))).then(collect);
    expect(seen.authorization).toEqual([`Bearer ${token}`]);
  });

  it('returns a typed unauthorized error without credentials', async () => {
    const result = await toy.streamThingsSse(createClient({ baseURL }), { id: 'abc' });
    const error = result.error!;
    if (!isGoaError(error)) throw error;
    expect(error.name).toBe('unauthorized');
    expect(error.status).toBe(401);
    expect(error.message).toBe('invalid token');
  });

  it('returns a typed unauthorized error with the wrong token', async () => {
    const result = await toy.streamThingsSse(client({ auth: 'wrong' }), { id: 'abc' });
    expect(result.error).toMatchObject({ kind: 'goa', name: 'unauthorized', status: 401 });
  });

  it('asks an async provider for the token on every call', async () => {
    let calls = 0;
    const authed = client({
      auth: async () => {
        calls++;
        await new Promise((resolve) => setTimeout(resolve, 1));
        return token;
      },
    });
    expect(await collect(await unwrapAsync(toy.streamCountSse(authed)))).toEqual([1, 2, 3]);
    expect(await collect(await unwrapAsync(toy.streamCountSse(authed)))).toEqual([1, 2, 3]);
    expect(calls).toBe(2);
  });

  it('selects the provider by scheme name', async () => {
    const authed = client({ auth: { jwt: token, api_key: 'wrong' } });
    expect((await toy.streamCountSse(authed)).ok).toBe(true);
    expect((await toy.streamKeyedSse(authed)).error).toMatchObject({ name: 'unauthorized' });
  });
});

describe('a method without security', () => {
  it('sends no Authorization header even when the client has a token', async () => {
    const seen = spy();
    const events = await collect(await unwrapAsync(toy.streamPublicSse(client({ middleware: [seen.middleware] }))));
    expect(seen.authorization).toEqual([null]);
    expect(events).toEqual([{ type: 'public', id: 'public-1', auth_seen: false }]);
  });
});

describe('API key security', () => {
  it('sends the key in the query string', async () => {
    const seen = spy();
    const authed = client({ auth: { api_key: token }, middleware: [seen.middleware] });
    const events = await collect(await unwrapAsync(toy.streamKeyedSse(authed)));
    expect(events.map((e) => e.id)).toEqual(['keyed-1']);
    expect(seen.urls).toEqual([`${baseURL}/keyed?key=${token}`]);
    expect(seen.authorization).toEqual([null]);
  });
});

describe('services share types but not names', () => {
  it('exposes each service as its own namespace and subpath', async () => {
    expect(echo.streamThingsSse).toBe(echoModule.streamThingsSse);
    expect(echo.streamThingsSse).not.toBe(toy.streamThingsSse);
    expectTypeOf<echo.StreamThingsSsePayload>().toEqualTypeOf<{ id: string }>();
    expectTypeOf<toy.StreamThingsSsePayload>().not.toEqualTypeOf<echo.StreamThingsSsePayload>();

    const events = await collect(await unwrapAsync(echo.streamThingsSse(client(), { id: 'hi' })));
    expect(events).toEqual([{ type: 'echo', id: 'echo-hi' }]);
    expectTypeOf(events).toEqualTypeOf<ThingEvent[]>();
    expectTypeOf(events[0]!.thing).toEqualTypeOf<Thing | undefined>();
    expect(ThingEventSchema.safeParse({ id: 'x' }).success).toBe(true);
  });

  it("applies the service's path prefix", async () => {
    const seen = spy();
    await unwrapAsync(echo.streamThingsSse(client({ middleware: [seen.middleware] }), { id: 'a b' })).then(collect);
    expect(seen.urls).toEqual([`${baseURL}/echo/things/a%20b/events`]);
  });
});

describe('transport failures', () => {
  it('returns NetworkError when nothing is listening', async () => {
    const result = await toy.streamCountSse(createClient({ baseURL: 'http://127.0.0.1:1', auth: token }));
    expect(result.error).toBeInstanceOf(NetworkError);
    expect(result.error).toMatchObject({ kind: 'network' });
  });

  it('returns UnexpectedResponseError for a response that is not an event stream', async () => {
    const hijacked = client({
      middleware: [async () => new Response('<html>hello</html>', { headers: { 'content-type': 'text/html' } })],
    });
    const result = await toy.streamCountSse(hijacked);
    expect(result.error).toBeInstanceOf(UnexpectedResponseError);
    expect(result.error).toMatchObject({ kind: 'unexpected_response', status: 200, body: '<html>hello</html>' });
  });

  it("returns UnexpectedResponseError for an error that is not goa's", async () => {
    const proxied = client({
      middleware: [async () => new Response('Bad Gateway', { status: 502, headers: { 'content-type': 'text/plain' } })],
    });
    const result = await toy.streamCountSse(proxied);
    expect(result.error).toBeInstanceOf(UnexpectedResponseError);
    expect(result.error).toMatchObject({ status: 502, body: 'Bad Gateway' });
  });
});
