import { describe, expect, it } from "vitest";
import * as z from "zod";

import {
  AbortError,
  bytes,
  dateTime,
  EventStream,
  NetworkError,
  ResponseValidationError,
  sseDecoder,
  type SseFrame,
  type SseMapping,
} from "../src/index.js";

const utf8 = new TextEncoder();

/** sseResponse streams the given chunks, then closes (or runs `after`). */
function sseResponse(
  chunks: (string | Uint8Array)[],
  after?: (controller: ReadableStreamDefaultController<Uint8Array>) => void,
): Response {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(typeof chunk === "string" ? utf8.encode(chunk) : chunk);
      }
      if (after) {
        after(controller);
      } else {
        controller.close();
      }
    },
  });
  return new Response(body, { headers: { "content-type": "text/event-stream" } });
}

const raw = (frame: SseFrame): SseFrame => frame;

async function collect<T>(stream: EventStream<T>): Promise<T[]> {
  const out: T[] = [];
  for await (const event of stream) {
    out.push(event);
  }
  return out;
}

const whole: SseMapping = { encoding: "json", data: null, id: null, event: null, retry: null };

describe("EventStream", () => {
  it("yields every event then ends when the server closes", async () => {
    const stream = new EventStream(sseResponse(["data: a\n\ndata: b\n\n", "data: c\n\n"]), raw);
    expect(await collect(stream)).toEqual([{ data: "a" }, { data: "b" }, { data: "c" }]);
  });

  it("reassembles UTF-8 sequences split across chunks", async () => {
    const bytesOf = utf8.encode("data: héllo \u{1f600}\n\n");
    const chunks = Array.from(bytesOf, (b) => new Uint8Array([b]));
    const stream = new EventStream(sseResponse(chunks), raw);
    expect(await collect(stream)).toEqual([{ data: "héllo \u{1f600}" }]);
  });

  it("strips a leading byte order mark", async () => {
    const stream = new EventStream(sseResponse([new Uint8Array([0xef, 0xbb, 0xbf]), "data: a\n\n"]), raw);
    expect(await collect(stream)).toEqual([{ data: "a" }]);
  });

  it("exposes lastEventId and retry as events are yielded", async () => {
    const stream = new EventStream(sseResponse(["retry: 300\nid: 1\ndata: a\n\nid: 2\ndata: b\n\n"]), raw);
    expect(stream.lastEventId).toBeUndefined();
    const seen: (string | undefined)[] = [];
    for await (const _ of stream) {
      seen.push(stream.lastEventId);
    }
    expect(seen).toEqual(["1", "2"]);
    expect(stream.lastEventId).toBe("2");
    expect(stream.retry).toBe(300);
  });

  it("cancels the response when the consumer breaks out", async () => {
    let cancelled = false;
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(utf8.encode("data: a\n\ndata: b\n\n"));
      },
      cancel() {
        cancelled = true;
      },
    });
    const stream = new EventStream(new Response(body), raw);
    for await (const event of stream) {
      expect(event.data).toBe("a");
      break;
    }
    expect(cancelled).toBe(true);
    expect(await collect(stream)).toEqual([]);
  });

  it("ends a pending iteration cleanly on close()", async () => {
    const stream = new EventStream(sseResponse(["data: a\n\n"], () => {}), raw);
    const iterator = stream[Symbol.asyncIterator]();
    expect((await iterator.next()).value).toEqual({ data: "a" });
    const pending = iterator.next();
    stream.close();
    expect(await pending).toEqual({ done: true, value: undefined });
  });

  it("throws AbortError when the request signal aborts mid-stream", async () => {
    const stream = new EventStream(
      sseResponse(["data: a\n\n"], (controller) => {
        queueMicrotask(() => controller.error(new DOMException("aborted", "AbortError")));
      }),
      raw,
    );
    const iterator = stream[Symbol.asyncIterator]();
    expect((await iterator.next()).value).toEqual({ data: "a" });
    await expect(iterator.next()).rejects.toBeInstanceOf(AbortError);
    expect(await iterator.next()).toEqual({ done: true, value: undefined });
  });

  it("throws NetworkError when the connection fails mid-stream", async () => {
    const stream = new EventStream(
      sseResponse([], (controller) => controller.error(new TypeError("terminated"))),
      raw,
    );
    await expect(collect(stream)).rejects.toBeInstanceOf(NetworkError);
  });

  it("throws ResponseValidationError and closes when an event fails to decode", async () => {
    const schema = z.object({ n: z.int() });
    const stream = new EventStream(
      sseResponse(['data: {"n":1}\n\ndata: {"n":"x"}\n\ndata: {"n":3}\n\n']),
      sseDecoder(schema, whole),
    );
    const iterator = stream[Symbol.asyncIterator]();
    expect((await iterator.next()).value).toEqual({ n: 1 });
    await expect(iterator.next()).rejects.toBeInstanceOf(ResponseValidationError);
    expect(await iterator.next()).toEqual({ done: true, value: undefined });
  });

  it("requires a response body", () => {
    expect(() => new EventStream(new Response(null), raw)).toThrow(TypeError);
  });
});

describe("sseDecoder", () => {
  it("decodes a whole JSON event through the schema codecs", () => {
    const schema = z.object({ id: z.string(), at: dateTime() });
    const decode = sseDecoder(schema, whole);
    expect(decode({ data: '{"id":"1","at":"2026-01-02T03:04:05Z"}' })).toEqual({
      id: "1",
      at: new Date("2026-01-02T03:04:05Z"),
    });
  });

  it("places data in its attribute and overlays id, event and retry", () => {
    const schema = z.object({
      data: z.object({ n: z.int() }),
      id: z.string().optional(),
      event: z.string().optional(),
      retry: z.int().optional(),
    });
    const decode = sseDecoder(schema, { encoding: "json", data: "data", id: "id", event: "event", retry: "retry" });
    expect(decode({ data: '{"n":1}', id: "7", event: "tick", retry: 250 })).toEqual({
      data: { n: 1 },
      id: "7",
      event: "tick",
      retry: 250,
    });
    expect(decode({ data: '{"n":2}' })).toEqual({ data: { n: 2 } });
  });

  it("overlays the frame id onto a whole-event object", () => {
    const schema = z.object({ id: z.string(), n: z.int() });
    const decode = sseDecoder(schema, { ...whole, id: "id" });
    expect(decode({ data: '{"id":"stale","n":1}', id: "fresh" })).toEqual({ id: "fresh", n: 1 });
  });

  it("decodes primitive events", () => {
    const of = (encoding: SseMapping["encoding"]): SseMapping => ({ ...whole, encoding });
    expect(sseDecoder(z.string(), of("string"))({ data: "line one\nline two" })).toBe("line one\nline two");
    expect(sseDecoder(z.string(), of("string"))({ data: '{"not":"parsed"}' })).toBe('{"not":"parsed"}');
    expect(sseDecoder(z.boolean(), of("boolean"))({ data: "true" })).toBe(true);
    expect(sseDecoder(z.boolean(), of("boolean"))({ data: "false" })).toBe(false);
    expect(sseDecoder(z.int(), of("int"))({ data: "-42" })).toBe(-42);
    expect(sseDecoder(z.number(), of("float"))({ data: "1.5e+06" })).toBe(1.5e6);
    expect(sseDecoder(bytes(), of("bytes"))({ data: "hé" })).toEqual(utf8.encode("hé"));
  });

  it("decodes a primitive data attribute", () => {
    const schema = z.object({ count: z.int(), id: z.string().optional() });
    const decode = sseDecoder(schema, { encoding: "int", data: "count", id: "id", event: null, retry: null });
    expect(decode({ data: "3", id: "a" })).toEqual({ count: 3, id: "a" });
  });

  it("rejects data that does not match its encoding", () => {
    const of = (encoding: SseMapping["encoding"]): SseMapping => ({ ...whole, encoding });
    expect(() => sseDecoder(z.int(), of("int"))({ data: "1.5" })).toThrow(ResponseValidationError);
    expect(() => sseDecoder(z.boolean(), of("boolean"))({ data: "yes" })).toThrow(ResponseValidationError);
    expect(() => sseDecoder(z.number(), of("float"))({ data: "" })).toThrow(ResponseValidationError);
    expect(() => sseDecoder(z.object({}), whole)({ data: "{oops" })).toThrow(ResponseValidationError);
  });
});
