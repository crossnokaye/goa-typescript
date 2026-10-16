import { describe, expect, it } from 'vitest';

import {
  AbortError,
  type ClientOptions,
  compose,
  createClient,
  type EndpointSpec,
  type Middleware,
  NetworkError,
  send,
  TimeoutError,
  unwrap,
} from '../src/index.js';

const base: EndpointSpec = {
  method: 'GET',
  path: '/things/{id}',
  pathParams: { id: 'id' },
  queryParams: {},
  headerParams: {},
  accept: 'application/json',
  security: [],
};

/** recorder returns a fetch that captures requests and answers 200. */
function recorder(): { fetch: typeof fetch; requests: Request[] } {
  const requests: Request[] = [];
  const impl = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    requests.push(new Request(input, init));
    return new Response('ok');
  };
  return { fetch: impl as typeof fetch, requests };
}

async function sent(
  spec: Partial<EndpointSpec>,
  wire: unknown,
  options: Partial<ClientOptions> = {},
  call?: Parameters<typeof send>[3],
): Promise<Request> {
  const rec = recorder();
  const client = createClient({ baseURL: 'http://toy.test/api', fetch: rec.fetch, ...options });
  unwrap(await send(client, { ...base, ...spec }, wire, call));
  return rec.requests[0]!;
}

describe('send: request construction', () => {
  it('builds the URL from path, query and map-query parameters', async () => {
    const req = await sent(
      {
        pathParams: { thing_id: 'id' },
        queryParams: { page_size: 'pageSize', tags: 'tag', skipped: 'skipped' },
        mapQuery: 'filters',
      },
      { thing_id: 'a/b', page_size: 10, tags: ['x', 'y'], filters: { color: 'red' } },
    );
    expect(req.method).toBe('GET');
    expect(req.url).toBe('http://toy.test/api/things/a%2Fb?pageSize=10&tag=x&tag=y&color=red');
    expect(req.headers.get('accept')).toBe('application/json');
    expect(req.body).toBeNull();
  });

  it('maps request properties to headers', async () => {
    const req = await sent(
      { headerParams: { request_id: 'X-Request-Id', flags: 'X-Flags', missing: 'X-Missing' } },
      { id: '1', request_id: 'r-1', flags: ['a', 'b'] },
    );
    expect(req.headers.get('x-request-id')).toBe('r-1');
    expect(req.headers.get('x-flags')).toBe('a, b');
    expect(req.headers.has('x-missing')).toBe(false);
  });

  it('sends the selected fields as a JSON body', async () => {
    const req = await sent(
      { method: 'POST', body: { kind: 'fields', fields: ['name', 'count', 'absent'] } },
      { id: '1', name: 'n', count: 2 },
    );
    expect(req.headers.get('content-type')).toBe('application/json');
    expect(await req.json()).toEqual({ name: 'n', count: 2 });
  });

  it('sends one attribute as the body, or no body when it is absent', async () => {
    const spec: Partial<EndpointSpec> = {
      method: 'POST',
      path: '/things',
      pathParams: {},
      body: { kind: 'attr', attr: 'filter' },
    };
    const withBody = await sent(spec, { filter: { kinds: ['a'] } });
    expect(await withBody.json()).toEqual({ kinds: ['a'] });
    const without = await sent(spec, {});
    expect(without.body).toBeNull();
    expect(without.headers.has('content-type')).toBe(false);
  });

  it('sends a non-object request whole', async () => {
    const body = await sent({ method: 'POST', path: '/things', pathParams: {}, body: { kind: 'whole' } }, ['a', 'b']);
    expect(await body.json()).toEqual(['a', 'b']);
    const path = await sent({}, 'abc');
    expect(path.url).toBe('http://toy.test/api/things/abc');
    const mapQuery = await sent({ path: '/things', pathParams: {}, mapQuery: '' }, { a: '1', b: '2' });
    expect(mapQuery.url).toBe('http://toy.test/api/things?a=1&b=2');
  });

  it('merges headers: client, then endpoint, then call', async () => {
    const req = await sent(
      { headerParams: { trace: 'X-Trace' } },
      { id: '1', trace: 'endpoint' },
      { headers: { 'X-Trace': 'client', 'X-Client': 'c', Accept: 'text/plain' } },
      { headers: { 'X-Trace': 'call', 'X-Call': '1' } },
    );
    expect(req.headers.get('x-trace')).toBe('call');
    expect(req.headers.get('x-client')).toBe('c');
    expect(req.headers.get('x-call')).toBe('1');
    expect(req.headers.get('accept')).toBe('application/json');
  });

  it('sends Last-Event-ID only when one is given', async () => {
    expect((await sent({}, { id: '1' }, {}, { lastEventId: '42' })).headers.get('last-event-id')).toBe('42');
    expect((await sent({}, { id: '1' }, {}, { lastEventId: '' })).headers.has('last-event-id')).toBe(false);
    expect((await sent({}, { id: '1' })).headers.has('last-event-id')).toBe(false);
  });

  it('honours a per-call base URL and passes RequestInit through', async () => {
    const req = await sent({}, { id: '1' }, { credentials: 'include' }, { baseURL: 'http://other.test', cache: 'no-store' });
    expect(req.url).toBe('http://other.test/things/1');
    expect(req.cache).toBe('no-store');
    expect(req.credentials).toBe('include');
    const override = await sent({}, { id: '1' }, { credentials: 'include' }, { credentials: 'omit' });
    expect(override.credentials).toBe('omit');
  });
});

describe('send: security', () => {
  const jwt = { scheme: 'jwt', kind: 'bearer', in: 'header', name: 'Authorization' } as const;

  it('prefixes a bare bearer token', async () => {
    const req = await sent({ security: [jwt] }, { id: '1' }, { auth: 'tok' });
    expect(req.headers.get('authorization')).toBe('Bearer tok');
  });

  it('sends a token that already names a scheme unchanged', async () => {
    const req = await sent({ security: [jwt] }, { id: '1' }, { auth: 'Custom tok' });
    expect(req.headers.get('authorization')).toBe('Custom tok');
  });

  it('invokes an async provider for every request', async () => {
    let calls = 0;
    const rec = recorder();
    const client = createClient({
      baseURL: 'http://toy.test',
      fetch: rec.fetch,
      auth: async () => `tok-${++calls}`,
    });
    await send(client, { ...base, security: [jwt] }, { id: '1' });
    await send(client, { ...base, security: [jwt] }, { id: '1' });
    expect(rec.requests.map((r) => r.headers.get('authorization'))).toEqual(['Bearer tok-1', 'Bearer tok-2']);
  });

  it('sends no credential when the provider has none or the endpoint is public', async () => {
    expect((await sent({ security: [jwt] }, { id: '1' }, { auth: () => undefined })).headers.has('authorization')).toBe(false);
    expect((await sent({ security: [jwt] }, { id: '1' })).headers.has('authorization')).toBe(false);
    expect((await sent({ security: [] }, { id: '1' }, { auth: 'tok' })).headers.has('authorization')).toBe(false);
  });

  it('selects providers by scheme name', async () => {
    const req = await sent(
      {
        security: [
          jwt,
          { scheme: 'api_key', kind: 'apiKey', in: 'header', name: 'X-API-Key' },
          { scheme: 'query_key', kind: 'apiKey', in: 'query', name: 'key' },
          { scheme: 'unconfigured', kind: 'apiKey', in: 'query', name: 'other' },
        ],
      },
      { id: '1' },
      { auth: { jwt: 'j', api_key: () => 'k', query_key: 'q k' } },
    );
    expect(req.headers.get('authorization')).toBe('Bearer j');
    expect(req.headers.get('x-api-key')).toBe('k');
    expect(req.url).toBe('http://toy.test/api/things/1?key=q+k');
  });

  it('does not prefix bearer tokens outside the Authorization header', async () => {
    const header = await sent(
      { security: [{ scheme: 'jwt', kind: 'bearer', in: 'header', name: 'X-Token' }] },
      { id: '1' },
      { auth: 'tok' },
    );
    expect(header.headers.get('x-token')).toBe('tok');
    const query = await sent(
      { security: [{ scheme: 'jwt', kind: 'bearer', in: 'query', name: 't' }] },
      { id: '1' },
      { auth: 'tok' },
    );
    expect(query.url).toBe('http://toy.test/api/things/1?t=tok');
  });

  it('encodes basic credentials', async () => {
    const req = await sent({ security: [{ scheme: 'basic', kind: 'basic' }] }, { id: '1' }, { auth: 'user:päss' });
    expect(req.headers.get('authorization')).toBe(`Basic ${Buffer.from('user:päss').toString('base64')}`);
  });

  it('lets a per-call Authorization header win', async () => {
    const req = await sent({ security: [jwt] }, { id: '1' }, { auth: 'tok' }, { headers: { Authorization: 'Bearer mine' } });
    expect(req.headers.get('authorization')).toBe('Bearer mine');
  });

  it('reports a failing provider as a network error', async () => {
    const client = createClient({
      baseURL: 'http://toy.test',
      fetch: recorder().fetch,
      auth: () => Promise.reject(new Error('no session')),
    });
    const result = await send(client, { ...base, security: [jwt] }, { id: '1' });
    expect(result.error).toBeInstanceOf(NetworkError);
    expect((result.error?.cause as Error).message).toBe('no session');
  });
});

describe('send: middleware', () => {
  it('runs onion style, first entry outermost', async () => {
    const order: string[] = [];
    const tag =
      (name: string): Middleware =>
      async (req, next) => {
        order.push(`${name}:before`);
        const headers = new Headers(req.headers);
        headers.set('x-trail', [req.headers.get('x-trail'), name].filter(Boolean).join(','));
        const res = await next(new Request(req, { headers }));
        order.push(`${name}:after`);
        return res;
      };
    const req = await sent({}, { id: '1' }, { middleware: [tag('a'), tag('b')] });
    expect(order).toEqual(['a:before', 'b:before', 'b:after', 'a:after']);
    expect(req.headers.get('x-trail')).toBe('a,b');
  });

  it('lets a middleware short-circuit', async () => {
    const rec = recorder();
    const client = createClient({
      baseURL: 'http://toy.test',
      fetch: rec.fetch,
      middleware: [async () => new Response('cached', { status: 203 })],
    });
    const res = unwrap(await send(client, base, { id: '1' }));
    expect(res.status).toBe(203);
    expect(rec.requests).toHaveLength(0);
  });

  it('composes with no middleware', async () => {
    const run = compose([], async () => new Response('terminal'));
    expect(await (await run(new Request('http://toy.test'))).text()).toBe('terminal');
  });
});

describe('send: failures', () => {
  /** hanging returns a fetch that never answers but honours the abort signal. */
  const hanging: typeof fetch = (input, init) =>
    new Promise((_resolve, reject) => {
      const signal = init?.signal ?? (input instanceof Request ? input.signal : undefined);
      const abort = (): void => reject(new DOMException('aborted', 'AbortError'));
      if (signal?.aborted) {
        abort();
      }
      signal?.addEventListener('abort', abort, { once: true });
    });

  it('maps a caller abort to AbortError', async () => {
    const controller = new AbortController();
    const client = createClient({ baseURL: 'http://toy.test', fetch: hanging });
    const pending = send(client, base, { id: '1' }, { signal: controller.signal });
    controller.abort();
    expect((await pending).error).toBeInstanceOf(AbortError);
  });

  it('maps an already-aborted signal to AbortError', async () => {
    const client = createClient({ baseURL: 'http://toy.test', fetch: hanging });
    const result = await send(client, base, { id: '1' }, { signal: AbortSignal.abort() });
    expect(result.error).toBeInstanceOf(AbortError);
  });

  it('maps a header timeout to TimeoutError', async () => {
    const client = createClient({ baseURL: 'http://toy.test', fetch: hanging, timeoutMs: 5 });
    const result = await send(client, base, { id: '1' });
    expect(result.error).toBeInstanceOf(TimeoutError);
    const perCall = await send(createClient({ baseURL: 'http://toy.test', fetch: hanging }), base, { id: '1' }, { timeoutMs: 5 });
    expect(perCall.error).toBeInstanceOf(TimeoutError);
  });

  it("still honours the caller's signal when a timeout is set", async () => {
    const controller = new AbortController();
    const client = createClient({ baseURL: 'http://toy.test', fetch: hanging, timeoutMs: 10_000 });
    const pending = send(client, base, { id: '1' }, { signal: controller.signal });
    controller.abort();
    expect((await pending).error).toBeInstanceOf(AbortError);
  });

  it('does not abort a response that arrived before the timeout', async () => {
    let signal: AbortSignal | undefined;
    const client = createClient({
      baseURL: 'http://toy.test',
      timeoutMs: 5,
      fetch: (async (input: RequestInfo | URL) => {
        signal = (input as Request).signal;
        return new Response('ok');
      }) as typeof fetch,
    });
    unwrap(await send(client, base, { id: '1' }));
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(signal?.aborted).toBe(false);
  });

  it('maps a fetch failure to NetworkError', async () => {
    const client = createClient({
      baseURL: 'http://toy.test',
      fetch: async () => {
        throw new TypeError('fetch failed');
      },
    });
    const result = await send(client, base, { id: '1' });
    expect(result.error).toBeInstanceOf(NetworkError);
    expect(result.error?.message).toBe('fetch failed');
    expect(result.error?.kind).toBe('network');
  });
});
