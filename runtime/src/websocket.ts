import type * as z from 'zod';

import { prepareRequest, type Client, type EndpointSpec, type WebSocketFactory } from './client.js';
import {
  AbortError,
  NetworkError,
  type RequestValidationError,
  type ResponseValidationError,
  TimeoutError,
  type TransportError,
} from './errors.js';
import { err, ok, type CallResult, type Result } from './result.js';
import { decodeJSON, encodeJSON } from './schema.js';

/** WebSocketOptions tune a single WebSocket call. */
export interface WebSocketOptions {
  /** signal aborts the connection attempt, or closes the open socket (code 1000). */
  signal?: AbortSignal | undefined;
  /** timeoutMs bounds the wait for the socket to open; the open socket is never cut off. */
  timeoutMs?: number | undefined;
  baseURL?: string | undefined;
  /**
   * headers only reach a `ClientOptions.webSocket` factory: the browser
   * `WebSocket` API cannot send headers.
   */
  headers?: HeadersInit | undefined;
  /** protocols are the subprotocols offered in the handshake. */
  protocols?: string | string[] | undefined;
}

/** WebSocketCloseInfo describes the close event that ended a socket. */
export interface WebSocketCloseInfo {
  code: number;
  reason: string;
  wasClean: boolean;
}

/** WebSocketCodec turns frames into values and values into frames for one endpoint. */
export interface WebSocketCodec<Recv, Send> {
  /** decode reads the text of one frame. */
  decode: ((text: string) => Result<Recv, ResponseValidationError>) | null;
  /** encode writes one message; null when the method streams no payload. */
  encode: ((msg: Send) => Result<string, RequestValidationError>) | null;
  /**
   * single is true when the server sends exactly one message after the
   * client ends its stream, then drops the connection without a close frame
   * (goa's client-streaming methods).
   */
  single: boolean;
}

/** WebSocketMapping is how a generated function describes its frames to `connectWebSocket`. */
export interface WebSocketMapping<R extends z.ZodType | null, S extends z.ZodType | null> {
  /** recv is the schema of the frames the server sends; null when it sends none. */
  recv: R;
  /** send is the schema of the frames the client sends; null when it sends none. */
  send: S;
  single: boolean;
}

type RecvOf<R> = R extends z.ZodType ? z.output<R> : never;
type SendOf<S> = S extends z.ZodType ? z.output<S> : never;

// WebSocket.readyState values, read from the instance's numbers rather than
// the global constructor, which a factory may not provide.
const OPEN = 1;
const CLOSING = 2;
const CLOSED = 3;

/**
 * WebSocketStream is an open goa WebSocket connection:
 *
 *     for await (const message of stream) { ... }
 *     stream.send(message);
 *     stream.end();
 *
 * Iterating yields the decoded messages the server sends. It ends when the
 * server closes the connection with code 1000, the way goa ends a stream, or
 * when the client closes it (`close`, leaving the loop). Any other close
 * throws a `NetworkError` carrying `closeCode` and `closeReason`. A message
 * that fails validation throws `ResponseValidationError` and closes the
 * socket; aborting the call's signal closes the socket and throws
 * `AbortError`.
 *
 * `Send` is `never` for a method that streams no payload, so `send` does not
 * type-check there.
 *
 * A stream can be iterated once, by one consumer at a time.
 */
export class WebSocketStream<Recv, Send = never> implements AsyncIterable<Recv> {
  /** socket is the underlying WebSocket; its listeners are owned by the stream. */
  readonly socket: WebSocket;
  /** closed resolves with the close event once the socket is closed. It never rejects. */
  readonly closed: Promise<WebSocketCloseInfo>;
  readonly #codec: WebSocketCodec<Recv, Send>;
  readonly #queue: unknown[] = [];
  readonly #wakers: (() => void)[] = [];
  readonly #signal: AbortSignal | undefined;
  readonly #onAbort = (): void => this.#abort();
  #closeInfo: WebSocketCloseInfo | undefined;
  #failure: Error | undefined;
  /** closing is true once the client asked to close: the close that follows is not a failure. */
  #closing = false;
  /** ended is true once end() sent goa's null: the server reads no further message. */
  #ended = false;
  #done = false;

  constructor(socket: WebSocket, codec: WebSocketCodec<Recv, Send>, signal?: AbortSignal) {
    this.socket = socket;
    this.#codec = codec;
    this.#signal = signal;
    let resolveClosed: (info: WebSocketCloseInfo) => void = () => {};
    this.closed = new Promise((resolve) => {
      resolveClosed = resolve;
    });
    socket.addEventListener('message', (event: MessageEvent) => {
      if (this.#done) {
        return;
      }
      this.#queue.push(event.data);
      this.#wake();
    });
    socket.addEventListener('close', (event: CloseEvent) => {
      this.#closeInfo = { code: event.code, reason: event.reason, wasClean: event.wasClean };
      signal?.removeEventListener('abort', this.#onAbort);
      resolveClosed(this.#closeInfo);
      this.#wake();
    });
    if (signal) {
      if (signal.aborted) {
        this.#abort();
      } else {
        signal.addEventListener('abort', this.#onAbort, { once: true });
      }
    }
  }

  /**
   * send validates and encodes a message, then sends it as one JSON text
   * frame. It returns the validation error rather than throwing, and a
   * `NetworkError` when the socket is no longer open.
   */
  send(msg: Send): Result<void, RequestValidationError | NetworkError> {
    if (!this.#codec.encode) {
      return err(new NetworkError('this method streams no messages to the server'));
    }
    if (this.#ended) {
      return err(new NetworkError('the stream was ended: the server reads no message after end()'));
    }
    if (this.socket.readyState !== OPEN) {
      return err(this.#notOpen());
    }
    const frame = this.#codec.encode(msg);
    if (!frame.ok) {
      return frame;
    }
    this.socket.send(frame.value);
    return ok(undefined);
  }

  /**
   * end tells the server that the client has nothing more to send, by
   * sending goa's `null` message. The socket stays open: keep iterating for
   * what the server sends next. A client-streaming method answers with its one
   * result, then the iteration ends.
   *
   * It only means something for methods that stream a payload; elsewhere the
   * server ignores it. Calling it again does nothing; `send` fails after it. It
   * returns a `NetworkError` when the socket is no longer open.
   */
  end(): Result<void, NetworkError> {
    if (this.#ended) {
      return ok(undefined);
    }
    if (this.socket.readyState !== OPEN) {
      return err(this.#notOpen());
    }
    this.#ended = true;
    this.socket.send('null');
    return ok(undefined);
  }

  /**
   * close closes the socket and ends the iteration; messages not yet read are
   * dropped. `code` must be 1000 or in 3000-4999, as the WebSocket API requires.
   */
  close(code = 1000, reason?: string): void {
    this.#finish();
    this.#closeSocket(code, reason);
  }

  [Symbol.asyncIterator](): AsyncIterator<Recv> {
    return {
      next: () => this.#next(),
      return: async () => {
        this.close();
        return { done: true, value: undefined };
      },
    };
  }

  async #next(): Promise<IteratorResult<Recv>> {
    for (;;) {
      if (this.#failure) {
        const failure = this.#failure;
        this.#failure = undefined;
        this.#finish();
        throw failure;
      }
      if (this.#done) {
        return { done: true, value: undefined };
      }
      if (this.#queue.length > 0) {
        return { done: false, value: this.#read(this.#queue.shift()) };
      }
      const info = this.#closeInfo;
      if (info) {
        this.#finish();
        if (info.code === 1000 || this.#closing) {
          return { done: true, value: undefined };
        }
        throw this.#closedError(info);
      }
      await new Promise<void>((resolve) => this.#wakers.push(resolve));
    }
  }

  /** read decodes one frame, closing the socket when the stream cannot go on. */
  #read(data: unknown): Recv {
    if (typeof data !== 'string') {
      this.close(1000, 'binary frame');
      throw new NetworkError('unexpected binary WebSocket frame: goa sends JSON text frames');
    }
    if (!this.#codec.decode) {
      this.close(1000, 'unexpected message');
      throw new NetworkError('unexpected WebSocket message: the method declares no result');
    }
    const value = this.#codec.decode(data);
    if (!value.ok) {
      this.close(1000, 'invalid message');
      throw value.error;
    }
    if (this.#codec.single) {
      // The one result: goa's server drops the connection right after it.
      this.close();
    }
    return value.value;
  }

  #abort(): void {
    if (this.#done) {
      return;
    }
    this.#failure = new AbortError('stream aborted', { cause: this.#signal?.reason });
    this.#queue.length = 0;
    this.#closeSocket(1000, 'aborted');
    this.#wake();
  }

  #finish(): void {
    this.#done = true;
    this.#queue.length = 0;
    this.#signal?.removeEventListener('abort', this.#onAbort);
    this.#wake();
  }

  #closeSocket(code: number, reason?: string): void {
    this.#closing = true;
    const state = this.socket.readyState;
    if (state !== CLOSING && state !== CLOSED) {
      this.socket.close(code, reason);
    }
  }

  #wake(): void {
    for (const wake of this.#wakers.splice(0)) {
      wake();
    }
  }

  #notOpen(): NetworkError {
    const info = this.#closeInfo;
    return info
      ? new NetworkError('the WebSocket is closed', { closeCode: info.code, closeReason: info.reason })
      : new NetworkError('the WebSocket is closing');
  }

  #closedError(info: WebSocketCloseInfo): NetworkError {
    const why = info.reason ? `: ${info.reason}` : '';
    return new NetworkError(`WebSocket closed with code ${info.code}${why}`, {
      closeCode: info.code,
      closeReason: info.reason,
    });
  }
}

/**
 * webSocketCodec builds the codec of an endpoint from the schemas of its
 * frames. Frames are JSON text, validated in both directions.
 */
export function webSocketCodec<R extends z.ZodType | null, S extends z.ZodType | null>(
  mapping: WebSocketMapping<R, S>,
): WebSocketCodec<RecvOf<R>, SendOf<S>> {
  const { recv, send } = mapping;
  return {
    decode: recv ? (text) => decodeJSON(recv, text) as Result<RecvOf<R>, ResponseValidationError> : null,
    encode: send ? (msg) => encodeJSON(send, msg as z.output<NonNullable<S>>) : null,
    single: mapping.single,
  };
}

/**
 * webSocketURL resolves a request URL against the page location (a relative
 * base URL works in a browser) and switches http(s) to ws(s).
 */
export function webSocketURL(url: string): string {
  const base = (globalThis as { location?: { href?: string } }).location?.href;
  const resolved = new URL(url, base);
  if (resolved.protocol === 'http:') {
    resolved.protocol = 'ws:';
  } else if (resolved.protocol === 'https:') {
    resolved.protocol = 'wss:';
  }
  return resolved.href;
}

const defaultFactory: WebSocketFactory = (url, protocols) => {
  const WebSocketImpl = (globalThis as { WebSocket?: typeof WebSocket }).WebSocket;
  if (typeof WebSocketImpl !== 'function') {
    throw new TypeError('no global WebSocket: set ClientOptions.webSocket');
  }
  return new WebSocketImpl(url, protocols);
};

/**
 * connectWebSocket opens the WebSocket of an endpoint. `wire` is the request
 * after schema encoding: its path and query parameters and its query
 * credentials go into the URL. Header parameters and header credentials only
 * reach a `ClientOptions.webSocket` factory, since the browser API cannot
 * send headers.
 *
 * It resolves once the socket is open. A connection that fails, including a
 * handshake the server refuses with an HTTP error, is a `NetworkError`: the
 * browser hides the status and body of that response. `result.response` is a
 * stand-in (an empty 200 `Response`) for the same reason.
 */
export async function connectWebSocket<R extends z.ZodType | null, S extends z.ZodType | null>(
  client: Client,
  spec: EndpointSpec,
  wire: unknown,
  options: WebSocketOptions | undefined,
  mapping: WebSocketMapping<R, S>,
): Promise<CallResult<WebSocketStream<RecvOf<R>, SendOf<S>>, TransportError>> {
  const { signal, timeoutMs: callTimeout, baseURL, headers, protocols } = options ?? {};
  if (signal?.aborted) {
    return { ok: false, error: new AbortError(undefined, { cause: signal.reason }) };
  }
  const prepared = await prepareRequest(client, spec, wire, { headers, baseURL });
  if (!prepared.ok) {
    return { ok: false, error: prepared.error };
  }
  // A credential provider may be slow: the signal can abort meanwhile.
  if (signal?.aborted) {
    return { ok: false, error: new AbortError(undefined, { cause: signal.reason }) };
  }

  let socket: WebSocket;
  let url: string;
  try {
    url = webSocketURL(prepared.value.url);
    socket = (client.options.webSocket ?? defaultFactory)(url, protocols, {
      headers: prepared.value.headers,
    });
  } catch (cause) {
    const message = cause instanceof Error ? cause.message : 'cannot open a WebSocket';
    return { ok: false, error: new NetworkError(message, { cause }) };
  }
  const stream = new WebSocketStream(socket, webSocketCodec(mapping), signal);

  const timeoutMs = callTimeout ?? client.options.timeoutMs;
  const opened = await new Promise<Result<void, TransportError>>((resolve) => {
    if (signal?.aborted) {
      // Aborted while the factory ran: the stream has closed the socket already.
      stream.close(1000, 'aborted');
      resolve(err(new AbortError(undefined, { cause: signal.reason })));
      return;
    }
    let timer: ReturnType<typeof setTimeout> | undefined;
    const settle = (result: Result<void, TransportError>): void => {
      socket.removeEventListener('open', onOpen);
      socket.removeEventListener('close', onClose);
      signal?.removeEventListener('abort', onAbort);
      if (timer !== undefined) {
        clearTimeout(timer);
      }
      resolve(result);
    };
    const onOpen = (): void => settle(ok(undefined));
    const onClose = (event: CloseEvent): void =>
      settle(
        err(
          new NetworkError(`WebSocket connection to ${redact(url)} failed (code ${event.code})`, {
            closeCode: event.code,
            closeReason: event.reason,
          }),
        ),
      );
    const onAbort = (): void => {
      stream.close(1000, 'aborted');
      settle(err(new AbortError(undefined, { cause: signal?.reason })));
    };
    socket.addEventListener('open', onOpen);
    socket.addEventListener('close', onClose);
    signal?.addEventListener('abort', onAbort, { once: true });
    if (timeoutMs && timeoutMs > 0) {
      timer = setTimeout(() => {
        stream.close(1000, 'timeout');
        settle(err(new TimeoutError(`WebSocket not open within ${timeoutMs}ms`)));
      }, timeoutMs);
    }
  });
  if (!opened.ok) {
    return { ok: false, error: opened.error };
  }
  // The browser exposes nothing of the handshake response, and the Response
  // constructor rejects its 101 status.
  return { ok: true, value: stream, response: new Response(null) };
}

/** redact drops the query string, which may hold a credential, from a URL for messages. */
function redact(url: string): string {
  const q = url.indexOf('?');
  return q === -1 ? url : url.slice(0, q);
}
