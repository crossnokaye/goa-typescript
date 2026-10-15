import {
  AbortError,
  NetworkError,
  RequestValidationError,
  type Tick,
  toy,
  unwrap,
  unwrapAsync,
  type WebSocketFactory,
} from "toy-client";
import { describe, expect, expectTypeOf, it } from "vitest";

import { client, collect, token } from "./helpers.js";

/**
 * NodeWebSocket is Node's global WebSocket (undici), whose constructor also
 * takes an init object with headers, which the DOM typings do not describe.
 */
const NodeWebSocket = WebSocket as unknown as new (
  url: string,
  init: { protocols?: string | string[] | undefined; headers: Record<string, string> },
) => WebSocket;

/** headerFactory opens sockets that send the headers the browser API cannot, and records them. */
function headerFactory(seen: Headers[]): WebSocketFactory {
  return (url, protocols, init) => {
    seen.push(init.headers);
    return new NodeWebSocket(url, { protocols, headers: Object.fromEntries(init.headers) });
  };
}

describe("watch_ticks: a server stream", () => {
  it("opens, yields every tick, then ends when the server closes with 1000", async () => {
    const result = await toy.watchTicks(client(), { id: "abc" });
    const stream = unwrap(result);
    const ticks = await collect(stream);
    expectTypeOf(ticks).toEqualTypeOf<Tick[]>();
    expect(ticks).toEqual([
      { n: 1, id: "abc", at: new Date("2026-01-02T03:04:01Z") },
      { n: 2, id: "abc", at: new Date("2026-01-02T03:04:02Z") },
      { n: 3, id: "abc", at: new Date("2026-01-02T03:04:03Z") },
    ]);
    expect(await stream.closed).toEqual({ code: 1000, reason: "server closing connection", wasClean: true });
  });

  it("sends query parameters, including the API key the browser can send", async () => {
    const stream = await unwrapAsync(toy.watchTicks(client(), { id: "q", count: 1 }));
    expect(new URL(stream.socket.url).searchParams.get("key")).toBe(token);
    expect(new URL(stream.socket.url).protocol).toBe("ws:");
    expect((await collect(stream)).map((t) => t.n)).toEqual([1]);
  });

  it("is refused without the right API key, as a NetworkError", async () => {
    const result = await toy.watchTicks(client({ auth: "wrong" }), { id: "abc" });
    expect(result.ok).toBe(false);
    expect(result.error).toBeInstanceOf(NetworkError);
    expect((result.error as NetworkError).closeCode).toBeTypeOf("number");
    expect(result.response).toBeUndefined();
  });

  it("reports an error the method returns before the upgrade as a NetworkError, not a goa error", async () => {
    const result = await toy.watchTicks(client(), { id: "missing" });
    expect(result.error).toBeInstanceOf(NetworkError);
    expect(result.error?.kind).toBe("network");
  });

  it("closes the socket with 1000 and throws AbortError when the signal aborts", async () => {
    const controller = new AbortController();
    const stream = await unwrapAsync(
      toy.watchTicks(client(), { id: "slow", count: 100, delay_ms: 20 }, { signal: controller.signal }),
    );
    const seen: number[] = [];
    await expect(
      (async () => {
        for await (const tick of stream) {
          seen.push(tick.n);
          controller.abort();
        }
      })(),
    ).rejects.toBeInstanceOf(AbortError);
    expect(seen).toEqual([1]);
    expect((await stream.closed).code).toBe(1000);
  });

  it("closes the socket when the consumer breaks out of the loop", async () => {
    const stream = await unwrapAsync(toy.watchTicks(client(), { id: "brk", count: 100, delay_ms: 20 }));
    for await (const tick of stream) {
      expect(tick.n).toBe(1);
      break;
    }
    expect((await stream.closed).code).toBe(1000);
    expect(await collect(stream)).toEqual([]);
  });

  it("does not let a server stream send", async () => {
    const stream = await unwrapAsync(toy.watchTicks(client(), { id: "abc", count: 1 }));
    // @ts-expect-error: watch_ticks streams no payload.
    expect(stream.send({ n: 1 }).ok).toBe(false);
    stream.close();
  });
});

describe("echo_ws: a bidirectional stream", () => {
  it("answers each message, then ends when the server closes after end()", async () => {
    const stream = await unwrapAsync(toy.echoWs(client(), { prefix: ">" }));
    expect(stream.send({ text: "ab", repeat: 2 })).toEqual({ ok: true, value: undefined });
    // repeat is left out: the server applies its default.
    expect(stream.send({ text: "c" }).ok).toBe(true);
    stream.end();
    expect(await collect(stream)).toEqual([
      { text: ">ABAB", length: 2 },
      { text: ">C", length: 1 },
    ]);
    expect((await stream.closed).code).toBe(1000);
  });

  it("interleaves sending and receiving, with no payload at all", async () => {
    const stream = await unwrapAsync(toy.echoWs(client()));
    const replies = stream[Symbol.asyncIterator]();
    stream.send({ text: "one" });
    expect(await replies.next()).toEqual({ done: false, value: { text: "ONE", length: 3 } });
    stream.send({ text: "two" });
    expect(await replies.next()).toEqual({ done: false, value: { text: "TWO", length: 3 } });
    stream.end();
    expect(await replies.next()).toEqual({ done: true, value: undefined });
  });

  it("returns the validation error of an invalid message and does not send it", async () => {
    const stream = await unwrapAsync(toy.echoWs(client()));
    const result = stream.send({ text: "" });
    expect(result.ok).toBe(false);
    expect(result.error).toBeInstanceOf(RequestValidationError);
    stream.send({ text: "ok" });
    stream.end();
    expect(await collect(stream)).toEqual([{ text: "OK", length: 2 }]);
  });

  it("throws a NetworkError carrying any close code other than 1000", async () => {
    const stream = await unwrapAsync(toy.echoWs(client()));
    stream.send({ text: "hi" });
    stream.send({ text: "kick" });
    const seen: string[] = [];
    const failure = await (async () => {
      for await (const reply of stream) {
        seen.push(reply.text);
      }
    })().catch((e: unknown) => e);
    expect(seen).toEqual(["HI"]);
    expect(failure).toBeInstanceOf(NetworkError);
    expect(failure).toMatchObject({ closeCode: 4001, closeReason: "kicked" });
    expect(stream.send({ text: "late" }).error).toBeInstanceOf(NetworkError);
  });
});

describe("sum_ints: a client stream", () => {
  it("cannot authenticate with the global WebSocket: the JWT travels in a header", async () => {
    const result = await toy.sumInts(client());
    expect(result.error).toBeInstanceOf(NetworkError);
  });

  it("authenticates through a webSocket factory that sends the headers, and yields the one result", async () => {
    const seen: Headers[] = [];
    const stream = await unwrapAsync(toy.sumInts(client({ webSocket: headerFactory(seen) })));
    expect(seen[0]?.get("authorization")).toBe(`Bearer ${token}`);
    for (const n of [1, 2, 3, 4]) {
      expect(stream.send(n).ok).toBe(true);
    }
    expect(stream.send(1.5).error).toBeInstanceOf(RequestValidationError);
    stream.end();
    // goa's server drops the connection right after the result; the
    // iteration ends after it all the same.
    expect(await collect(stream)).toEqual([{ sum: 10, count: 4 }]);
  });
});
