import type * as z from 'zod';

import { isAbort } from './client.js';
import { AbortError, NetworkError } from './errors.js';
import { bytesToBase64, decode, invalidJSON } from './schema.js';

/** SseFrame is one dispatched Server-Sent Event. */
export interface SseFrame {
  /** data is the event's `data:` lines joined with '\n'. */
  data: string;
  /** id is the last event ID in effect for this event, when the stream ever sent one. */
  id?: string;
  /** event is the event type, when this event named one. */
  event?: string;
  /** retry is the reconnection delay in milliseconds, when this event set one. */
  retry?: number;
}

/** FrameDecoder turns a frame into an event value, throwing when it is invalid. */
export type FrameDecoder<T> = (frame: SseFrame) => T;

/** SseParser consumes decoded text in arbitrary chunks. */
export interface SseParser {
  feed(chunk: string): void;
}

/**
 * createParser returns an incremental parser for the `text/event-stream`
 * format (WHATWG HTML, "Server-sent events"). Events without data are not
 * dispatched, and an event still open when the input ends is discarded.
 */
export function createParser(
  onEvent: (frame: SseFrame) => void,
  onRetry?: (retry: number) => void,
  onId?: (id: string) => void,
): SseParser {
  let started = false;
  // pendingCR is true when the last chunk ended in CR: a following LF belongs
  // to the same line ending.
  let pendingCR = false;
  let line = '';
  let data: string[] = [];
  let event: string | undefined;
  let retry: number | undefined;
  let lastEventId: string | undefined;

  function dispatch(): void {
    if (data.length > 0) {
      const frame: SseFrame = { data: data.join('\n') };
      if (lastEventId !== undefined) {
        frame.id = lastEventId;
      }
      if (event !== undefined) {
        frame.event = event;
      }
      if (retry !== undefined) {
        frame.retry = retry;
      }
      onEvent(frame);
    }
    data = [];
    event = undefined;
    retry = undefined;
  }

  function processLine(text: string): void {
    if (text === '') {
      dispatch();
      return;
    }
    if (text.startsWith(':')) {
      return;
    }
    const colon = text.indexOf(':');
    const field = colon === -1 ? text : text.slice(0, colon);
    let value = colon === -1 ? '' : text.slice(colon + 1);
    if (value.startsWith(' ')) {
      value = value.slice(1);
    }
    switch (field) {
      case 'data':
        data.push(value);
        break;
      case 'event':
        event = value;
        break;
      case 'id':
        if (!value.includes('\u0000')) {
          lastEventId = value;
          onId?.(value);
        }
        break;
      case 'retry':
        if (/^[0-9]+$/.test(value)) {
          retry = Number(value);
          onRetry?.(retry);
        }
        break;
      default:
        break;
    }
  }

  return {
    feed(chunk: string): void {
      if (chunk === '') {
        return;
      }
      if (!started) {
        started = true;
        if (chunk.charCodeAt(0) === 0xfeff) {
          chunk = chunk.slice(1);
        }
      }
      let start = 0;
      if (pendingCR) {
        pendingCR = false;
        if (chunk.startsWith('\n')) {
          start = 1;
        }
      }
      for (let i = start; i < chunk.length; i++) {
        const code = chunk.charCodeAt(i);
        if (code !== 0x0a && code !== 0x0d) {
          continue;
        }
        processLine(line + chunk.slice(start, i));
        line = '';
        if (code === 0x0d) {
          if (i + 1 === chunk.length) {
            pendingCR = true;
          } else if (chunk.charCodeAt(i + 1) === 0x0a) {
            i++;
          }
        }
        start = i + 1;
      }
      line += chunk.slice(start);
    },
  };
}

/**
 * EventStream iterates the events of one Server-Sent Events response:
 *
 *     for await (const event of stream) { ... }
 *
 * It does not reconnect. The iteration ends when the server closes the
 * stream; pass `stream.lastEventId` as `options.lastEventId` of a new call to
 * resume. Leaving the loop early (or calling `close`) cancels the response.
 * An event that fails validation throws `ResponseValidationError` and closes
 * the stream; aborting the request's signal throws `AbortError`.
 *
 * A stream can be iterated once.
 */
export class EventStream<T> implements AsyncIterable<T> {
  /** response is the underlying HTTP response; its body is owned by the stream. */
  readonly response: Response;
  readonly #decode: FrameDecoder<T>;
  readonly #reader: ReadableStreamDefaultReader<Uint8Array>;
  readonly #text = new TextDecoder('utf-8', { ignoreBOM: true });
  readonly #frames: SseFrame[] = [];
  readonly #parser: SseParser;
  #lastEventId: string | undefined;
  #retry: number | undefined;
  #done = false;
  #closed = false;

  constructor(response: Response, decoder: FrameDecoder<T>) {
    if (!response.body) {
      throw new TypeError('EventStream requires a response with a body');
    }
    this.response = response;
    this.#decode = decoder;
    this.#reader = response.body.getReader();
    this.#parser = createParser(
      (frame) => this.#frames.push(frame),
      (retry) => {
        this.#retry = retry;
      },
    );
  }

  /** lastEventId is the ID of the most recent event yielded, for resuming. */
  get lastEventId(): string | undefined {
    return this.#lastEventId;
  }

  /** retry is the reconnection delay in milliseconds most recently sent by the server. */
  get retry(): number | undefined {
    return this.#retry;
  }

  /** close cancels the response and ends the iteration. */
  close(): void {
    if (this.#closed) {
      return;
    }
    this.#closed = true;
    this.#done = true;
    this.#frames.length = 0;
    this.#reader.cancel().catch(() => {});
  }

  [Symbol.asyncIterator](): AsyncIterator<T> {
    return {
      next: () => this.#next(),
      return: async () => {
        this.close();
        return { done: true, value: undefined };
      },
    };
  }

  // The loop is hand-rolled over a reader because async iteration of
  // ReadableStream is missing before Safari 18.4.
  async #next(): Promise<IteratorResult<T>> {
    for (;;) {
      const frame = this.#frames.shift();
      if (frame) {
        if (frame.id !== undefined) {
          this.#lastEventId = frame.id;
        }
        try {
          return { done: false, value: this.#decode(frame) };
        } catch (cause) {
          this.close();
          throw cause;
        }
      }
      if (this.#done) {
        return { done: true, value: undefined };
      }
      let chunk: ReadableStreamReadResult<Uint8Array>;
      try {
        chunk = await this.#reader.read();
      } catch (cause) {
        const closed = this.#closed;
        this.#done = true;
        this.#closed = true;
        if (closed) {
          return { done: true, value: undefined };
        }
        if (isAbort(cause)) {
          throw new AbortError('stream aborted', { cause });
        }
        throw new NetworkError(cause instanceof Error ? cause.message : 'stream failed', { cause });
      }
      if (chunk.done) {
        // An event left open at end of input is discarded, per the SSE spec.
        this.#done = true;
        continue;
      }
      this.#parser.feed(this.#text.decode(chunk.value, { stream: true }));
    }
  }
}

/** SseDataEncoding says how goa wrote the event data. */
export type SseDataEncoding = 'json' | 'string' | 'boolean' | 'int' | 'float' | 'bytes';

/** SseMapping records how a design maps event attributes onto SSE fields. */
export interface SseMapping {
  encoding: SseDataEncoding;
  /** data is the attribute carried in the `data` field; null when the whole event is. */
  data: string | null;
  /** id, event and retry are the attributes carried in those SSE fields, if any. */
  id: string | null;
  event: string | null;
  retry: string | null;
}

/**
 * sseDecoder builds the decoder for an endpoint's events: it parses the data
 * per the mapping, overlays the frame's id/event/retry onto their attributes,
 * then decodes the result with the event schema.
 */
export function sseDecoder<T extends z.ZodType>(schema: T, mapping: SseMapping): FrameDecoder<z.output<T>> {
  return (frame) => {
    const data = parseData(frame.data, mapping.encoding);
    let wire: unknown = data;
    if (mapping.data !== null) {
      wire = { [mapping.data]: data };
    }
    if (typeof wire === 'object' && wire !== null && !Array.isArray(wire)) {
      const overlay: Record<string, unknown> = { ...(wire as Record<string, unknown>) };
      if (mapping.id !== null && frame.id !== undefined) {
        overlay[mapping.id] = frame.id;
      }
      if (mapping.event !== null && frame.event !== undefined) {
        overlay[mapping.event] = frame.event;
      }
      if (mapping.retry !== null && frame.retry !== undefined) {
        overlay[mapping.retry] = frame.retry;
      }
      wire = overlay;
    }
    const result = decode(schema, wire);
    if (!result.ok) {
      throw result.error;
    }
    return result.value;
  };
}

/** parseData reverses goa's formatting of event data. Unparseable text is left for the schema to reject. */
function parseData(text: string, encoding: SseDataEncoding): unknown {
  switch (encoding) {
    case 'string':
      return text;
    case 'boolean':
      return text === 'true' ? true : text === 'false' ? false : text;
    case 'int':
      return /^[+-]?[0-9]+$/.test(text) ? Number(text) : text;
    case 'float': {
      const value = text.trim() === '' ? Number.NaN : Number(text);
      return Number.isNaN(value) ? text : value;
    }
    case 'bytes':
      // goa writes the bytes raw; the Bytes schema expects base64.
      return bytesToBase64(new TextEncoder().encode(text));
    case 'json':
      try {
        return JSON.parse(text);
      } catch (cause) {
        throw invalidJSON(text, cause);
      }
  }
}
