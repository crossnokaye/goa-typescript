import * as z from 'zod';

/** ErrorResult is the JSON body of goa's default error type (`application/vnd.goa.error`). */
export interface ErrorResult {
  name: string;
  id: string;
  message: string;
  temporary: boolean;
  timeout: boolean;
  fault: boolean;
}

/**
 * OtherErrorName stands for goa error names that the design does not declare
 * for an endpoint (for example goa's built-in request validation errors). It
 * keeps declared names visible in editor completion.
 */
export type OtherErrorName = string & {};

/**
 * GoaError is an error returned by a goa service. `name` is the goa error name
 * and discriminates the per-endpoint error unions; `detail` carries the decoded
 * body of errors declared with a custom type.
 */
export class GoaError<Name extends string = string, Detail = undefined> extends Error {
  readonly kind = 'goa' as const;
  override readonly name: Name;
  /** id is the unique identifier goa assigned to this error occurrence. */
  readonly id: string;
  readonly temporary: boolean;
  readonly timeout: boolean;
  readonly fault: boolean;
  readonly status: number;
  readonly headers: Headers;
  /** body is the response body as parsed JSON, or as text when it is not JSON. */
  readonly body: unknown;
  readonly response: Response;
  readonly detail: Detail;

  constructor(init: {
    name: Name;
    message: string;
    id?: string;
    temporary?: boolean;
    timeout?: boolean;
    fault?: boolean;
    body: unknown;
    response: Response;
    detail: Detail;
  }) {
    super(init.message);
    this.name = init.name;
    this.id = init.id ?? '';
    this.temporary = init.temporary ?? false;
    this.timeout = init.timeout ?? false;
    this.fault = init.fault ?? false;
    this.status = init.response.status;
    this.headers = init.response.headers;
    this.body = init.body;
    this.response = init.response;
    this.detail = init.detail;
  }
}

/**
 * NetworkError reports that the request never produced a response, or that a
 * stream broke. For a WebSocket, `closeCode` and `closeReason` carry the close
 * event that ended it, when there was one (a browser reports 1006 for a
 * connection that dropped or a handshake the server refused).
 */
export class NetworkError extends Error {
  readonly kind = 'network' as const;
  override readonly name = 'NetworkError';
  readonly closeCode?: number;
  readonly closeReason?: string;

  constructor(message: string, options?: { cause?: unknown; closeCode?: number; closeReason?: string }) {
    super(message, options && 'cause' in options ? { cause: options.cause } : undefined);
    if (options?.closeCode !== undefined) {
      this.closeCode = options.closeCode;
      this.closeReason = options.closeReason ?? '';
    }
  }
}

/** AbortError reports that the caller's signal cancelled the request or stream. */
export class AbortError extends Error {
  readonly kind = 'abort' as const;
  override readonly name = 'AbortError';

  constructor(message = 'request aborted', options?: { cause?: unknown }) {
    super(message, options);
  }
}

/** TimeoutError reports that no response headers arrived within `timeoutMs`. */
export class TimeoutError extends Error {
  readonly kind = 'timeout' as const;
  override readonly name = 'TimeoutError';

  constructor(message = 'request timed out', options?: { cause?: unknown }) {
    super(message, options);
  }
}

export type TransportError = NetworkError | AbortError | TimeoutError;

/** RequestValidationError reports a request that does not satisfy the design. Nothing was sent. */
export class RequestValidationError extends Error {
  readonly kind = 'request_validation' as const;
  override readonly name = 'RequestValidationError';
  readonly issues: readonly z.core.$ZodIssue[];

  constructor(issues: readonly z.core.$ZodIssue[], options?: { cause?: unknown }) {
    super(prettyIssues('invalid request', issues), options);
    this.issues = issues;
  }

  /** pretty renders the issues one per line. */
  pretty(): string {
    return prettyIssues('invalid request', this.issues);
  }
}

/** ResponseValidationError reports a response that does not satisfy the design. */
export class ResponseValidationError extends Error {
  readonly kind = 'response_validation' as const;
  override readonly name = 'ResponseValidationError';
  readonly issues: readonly z.core.$ZodIssue[];

  constructor(issues: readonly z.core.$ZodIssue[], options?: { cause?: unknown }) {
    super(prettyIssues('invalid response', issues), options);
    this.issues = issues;
  }

  /** pretty renders the issues one per line. */
  pretty(): string {
    return prettyIssues('invalid response', this.issues);
  }
}

/**
 * UnexpectedResponseError reports a response whose status or content type the
 * design does not describe and whose body is not a goa error.
 */
export class UnexpectedResponseError extends Error {
  readonly kind = 'unexpected_response' as const;
  override readonly name = 'UnexpectedResponseError';
  readonly status: number;
  readonly headers: Headers;
  /** body is the raw response text. */
  readonly body: string;
  readonly response: Response;
  /** goa is set when the body still parses as goa's default error. */
  readonly goa?: ErrorResult;

  constructor(response: Response, body: string) {
    const goa = parseErrorResult(body);
    super(goa?.message ?? `unexpected response: HTTP ${response.status}`);
    this.status = response.status;
    this.headers = response.headers;
    this.body = body;
    this.response = response;
    if (goa) {
      this.goa = goa;
    }
  }
}

export type ClientError =
  TransportError | RequestValidationError | ResponseValidationError | UnexpectedResponseError;

/**
 * EndpointError is the error type of any generated function: every function's
 * error union narrows to it. Use it for code that handles errors from more than
 * one endpoint, such as a shared query-options alias or a toast formatter.
 */
export type EndpointError = GoaError<string, unknown> | ClientError;

/**
 * isGoaError reports whether e is an error returned by a goa service and, when
 * a name is given, whether it is the error of that name.
 *
 * With a name, and with e typed as the error union of an endpoint
 * (`result.error`), e narrows to the member the design declares under that
 * name, so `e.detail` has the type of the error. Comparing `e.name` does not
 * narrow that far: an undeclared error can carry any name.
 *
 * In a `catch` block e is `unknown` and there is no union to pick from: e
 * narrows to `GoaError<N, unknown>` and `detail` stays `unknown`.
 */
export function isGoaError(e: unknown): e is GoaError<string, unknown>;
export function isGoaError<E extends { readonly kind: string } | undefined, N extends string>(
  e: E,
  name: N,
): e is Extract<E, { readonly kind: 'goa'; readonly name: N }>;
export function isGoaError<N extends string>(e: unknown, name: N): e is GoaError<N, unknown>;
export function isGoaError(e: unknown, name?: string): boolean {
  return e instanceof GoaError && (name === undefined || e.name === name);
}

/** errorMessage returns a display string for anything a generated function can fail with. */
export function errorMessage(e: unknown): string {
  if (e instanceof RequestValidationError || e instanceof ResponseValidationError) {
    return e.pretty();
  }
  if (e instanceof Error) {
    return e.message;
  }
  if (typeof e === 'string') {
    return e;
  }
  try {
    return JSON.stringify(e) ?? String(e);
  } catch {
    return String(e);
  }
}

/** asErrorResult returns v when it has the shape of goa's default error. */
export function asErrorResult(v: unknown): ErrorResult | undefined {
  if (typeof v !== 'object' || v === null) {
    return undefined;
  }
  const o = v as Record<string, unknown>;
  if (typeof o.name !== 'string' || typeof o.message !== 'string') {
    return undefined;
  }
  return {
    name: o.name,
    id: typeof o.id === 'string' ? o.id : '',
    message: o.message,
    temporary: o.temporary === true,
    timeout: o.timeout === true,
    fault: o.fault === true,
  };
}

function parseErrorResult(body: string): ErrorResult | undefined {
  try {
    return asErrorResult(JSON.parse(body));
  } catch {
    return undefined;
  }
}

function prettyIssues(prefix: string, issues: readonly z.core.$ZodIssue[]): string {
  if (issues.length === 0) {
    return prefix;
  }
  return `${prefix}:\n${z.prettifyError({ issues: issues as z.core.$ZodIssue[] })}`;
}
