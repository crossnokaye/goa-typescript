import { AbortError, NetworkError, TimeoutError, type TransportError } from "./errors.js";
import { compose, type Middleware } from "./middleware.js";
import { appendMapQuery, appendQuery, buildURL, encodeHeaderValue, jsonBody } from "./request.js";
import { err, ok, type Result } from "./result.js";

/**
 * TokenProvider supplies a credential: a fixed string, or a function invoked
 * for every request (so it can return a refreshed token). Returning
 * `undefined` sends the request without that credential.
 *
 * For basic auth the credential is `username:password`.
 */
export type TokenProvider =
  | string
  | (() => string | undefined | Promise<string | undefined>);

/** SecurityScheme describes where one of the design's security schemes puts its credential. */
export type SecurityScheme =
  | { scheme: string; kind: "bearer"; in: "header" | "query"; name: string }
  | { scheme: string; kind: "apiKey"; in: "header" | "query"; name: string }
  | { scheme: string; kind: "basic" };

export interface ClientOptions {
  /** baseURL is prepended to every endpoint path; its own path prefix is kept. */
  baseURL: string;
  /**
   * auth supplies credentials: one provider used for every security scheme,
   * or a provider per scheme name as declared in the design.
   */
  auth?: TokenProvider | Record<string, TokenProvider>;
  /** fetch replaces the global fetch. */
  fetch?: typeof fetch;
  /** middleware wraps every request; the first entry is outermost. */
  middleware?: readonly Middleware[];
  /** headers are sent with every request. */
  headers?: HeadersInit;
  credentials?: RequestCredentials;
  /** timeoutMs bounds the wait for response headers. Streams are not cut off once they start. */
  timeoutMs?: number;
}

/** Client carries the configuration shared by every generated function. */
export interface Client {
  readonly options: ClientOptions;
}

/** createClient builds the client passed as the first argument of every generated function. */
export function createClient(options: ClientOptions): Client {
  return { options: { ...options } };
}

/** RequestOptions tune a single call. Unlisted `RequestInit` fields pass through to fetch. */
export interface RequestOptions
  extends Omit<RequestInit, "method" | "body" | "headers" | "signal"> {
  signal?: AbortSignal;
  /** headers are merged over the client's headers and the endpoint's own. */
  headers?: HeadersInit;
  timeoutMs?: number;
  baseURL?: string;
}

/** StreamOptions tune a single streaming call. */
export interface StreamOptions extends RequestOptions {
  /** lastEventId resumes a stream: it is sent as the `Last-Event-ID` header. */
  lastEventId?: string;
}

/** EndpointBody says which part of the encoded request becomes the JSON body. */
export type EndpointBody =
  | { kind: "fields"; fields: readonly string[] }
  | { kind: "attr"; attr: string }
  | { kind: "whole" };

/** EndpointSpec is the transport mapping of one goa HTTP endpoint. */
export interface EndpointSpec {
  method: string;
  /** path is the goa route, e.g. `/things/{id}` or `/files/{*path}`. */
  path: string;
  /** pathParams maps request properties to path parameter names. */
  pathParams: Readonly<Record<string, string>>;
  /** queryParams maps request properties to query string keys. */
  queryParams: Readonly<Record<string, string>>;
  /** headerParams maps request properties to header names. */
  headerParams: Readonly<Record<string, string>>;
  /** mapQuery names the request property whose entries spread into the query string ("" = the whole request). */
  mapQuery?: string;
  body?: EndpointBody;
  accept: string;
  security: readonly SecurityScheme[];
}

/**
 * send performs one HTTP exchange for an endpoint. `wire` is the request after
 * schema encoding. It resolves once response headers arrive; reading the body
 * is left to the caller.
 */
export async function send(
  client: Client,
  spec: EndpointSpec,
  wire: unknown,
  options?: StreamOptions,
): Promise<Result<Response, TransportError>> {
  const {
    signal: callerSignal,
    headers: callHeaders,
    timeoutMs: callTimeout,
    baseURL: callBaseURL,
    lastEventId,
    ...init
  } = options ?? {};
  const config = client.options;
  const timeoutMs = callTimeout ?? config.timeoutMs;

  // A non-object request (primitive, array or map payload) is itself the
  // value of whichever single element the design maps it to.
  const record = isRecord(wire) ? wire : undefined;
  const get = (prop: string): unknown => (record ? record[prop] : wire);

  const pathParams: Record<string, unknown> = {};
  for (const [prop, name] of Object.entries(spec.pathParams)) {
    pathParams[name] = get(prop);
  }
  const query = new URLSearchParams();
  for (const [prop, name] of Object.entries(spec.queryParams)) {
    appendQuery(query, name, get(prop));
  }
  if (spec.mapQuery !== undefined) {
    appendMapQuery(query, spec.mapQuery === "" ? wire : get(spec.mapQuery));
  }

  const headers = new Headers(config.headers);
  headers.set("Accept", spec.accept);
  for (const [prop, name] of Object.entries(spec.headerParams)) {
    const value = get(prop);
    if (value !== undefined && value !== null) {
      headers.set(name, encodeHeaderValue(value));
    }
  }
  if (lastEventId) {
    headers.set("Last-Event-ID", lastEventId);
  }

  let body: string | undefined;
  if (spec.body) {
    let value: unknown;
    switch (spec.body.kind) {
      case "fields": {
        const picked: Record<string, unknown> = {};
        for (const field of spec.body.fields) {
          const v = get(field);
          if (v !== undefined) {
            picked[field] = v;
          }
        }
        value = picked;
        break;
      }
      case "attr":
        value = get(spec.body.attr);
        break;
      case "whole":
        value = wire;
        break;
    }
    if (value !== undefined) {
      body = jsonBody(value);
      headers.set("Content-Type", "application/json");
    }
  }

  new Headers(callHeaders).forEach((value, name) => headers.set(name, value));

  try {
    for (const scheme of spec.security) {
      const token = await resolveToken(config.auth, scheme.scheme);
      if (!token) {
        continue;
      }
      applyCredential(scheme, token, headers, query);
    }
  } catch (cause) {
    return err(new NetworkError("credential provider failed", { cause }));
  }

  const url = buildURL(callBaseURL ?? config.baseURL, spec.path, pathParams, query);

  const timeout = timeoutMs && timeoutMs > 0 ? new AbortController() : undefined;
  let timedOut = false;
  const timer = timeout
    ? setTimeout(() => {
        timedOut = true;
        timeout.abort();
      }, timeoutMs)
    : undefined;
  const signal = anySignal(callerSignal, timeout?.signal);

  const requestInit: RequestInit = { ...init, method: spec.method, headers };
  if (config.credentials !== undefined && requestInit.credentials === undefined) {
    requestInit.credentials = config.credentials;
  }
  if (body !== undefined) {
    requestInit.body = body;
  }
  if (signal) {
    requestInit.signal = signal;
  }

  try {
    const fetchImpl = config.fetch ?? globalThis.fetch;
    const run = compose(config.middleware ?? [], (req) => fetchImpl(req));
    return ok(await run(new Request(url, requestInit)));
  } catch (cause) {
    if (timedOut) {
      return err(new TimeoutError(`no response within ${timeoutMs}ms`, { cause }));
    }
    if (callerSignal?.aborted || isAbort(cause)) {
      return err(new AbortError(undefined, { cause }));
    }
    return err(new NetworkError(cause instanceof Error ? cause.message : "network error", { cause }));
  } finally {
    // The timeout only bounds the wait for headers, so a stream outlives it.
    if (timer !== undefined) {
      clearTimeout(timer);
    }
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** isAbort reports whether a fetch or stream failure was caused by an abort signal. */
export function isAbort(cause: unknown): boolean {
  return typeof cause === "object" && cause !== null && (cause as { name?: unknown }).name === "AbortError";
}

async function resolveToken(
  auth: ClientOptions["auth"],
  scheme: string,
): Promise<string | undefined> {
  if (auth === undefined) {
    return undefined;
  }
  const provider = typeof auth === "string" || typeof auth === "function" ? auth : auth[scheme];
  if (provider === undefined) {
    return undefined;
  }
  return typeof provider === "function" ? await provider() : provider;
}

function applyCredential(
  scheme: SecurityScheme,
  token: string,
  headers: Headers,
  query: URLSearchParams,
): void {
  switch (scheme.kind) {
    case "basic":
      if (!headers.has("Authorization")) {
        headers.set("Authorization", `Basic ${base64UTF8(token)}`);
      }
      return;
    case "bearer":
    case "apiKey": {
      if (scheme.in === "query") {
        if (!query.has(scheme.name)) {
          query.set(scheme.name, token);
        }
        return;
      }
      if (headers.has(scheme.name)) {
        return;
      }
      // goa strips the scheme prefix server side; a token that already names
      // one (it contains a space) is sent as is.
      const bearer =
        scheme.kind === "bearer" && scheme.name.toLowerCase() === "authorization" && !token.includes(" ");
      headers.set(scheme.name, bearer ? `Bearer ${token}` : token);
      return;
    }
  }
}

function base64UTF8(text: string): string {
  let binary = "";
  for (const byte of new TextEncoder().encode(text)) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary);
}

/** anySignal aborts when either signal does; `AbortSignal.any` is missing before Safari 17.4. */
function anySignal(a?: AbortSignal, b?: AbortSignal): AbortSignal | undefined {
  if (!a || !b) {
    return a ?? b;
  }
  if (typeof AbortSignal.any === "function") {
    return AbortSignal.any([a, b]);
  }
  const controller = new AbortController();
  for (const signal of [a, b]) {
    if (signal.aborted) {
      controller.abort(signal.reason);
      break;
    }
    signal.addEventListener("abort", () => controller.abort(signal.reason), { once: true });
  }
  return controller.signal;
}
