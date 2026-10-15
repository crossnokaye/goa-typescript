import { afterEach, describe, expect, expectTypeOf, it, vi } from "vitest";
import * as z from "zod";

import {
  AbortError,
  type CallResult,
  type ClientOptions,
  connectWebSocket,
  createClient,
  dateTime,
  type EndpointSpec,
  NetworkError,
  RequestValidationError,
  ResponseValidationError,
  TimeoutError,
  type TransportError,
  type WebSocketMapping,
  type WebSocketOptions,
  type WebSocketStream,
  webSocketURL,
} from "../src/index.js";

/**
 * FakeSocket stands in for a browser WebSocket. Tests drive the server side
 * with open/message/serverClose; close() answers with a close event carrying
 * the client's code, as a server echoing the close frame does.
 */
class FakeSocket extends EventTarget {
  static last: FakeSocket | undefined;
  readyState = 0;
  readonly sent: string[] = [];
  closedWith: { code?: number | undefined; reason?: string | undefined } | undefined;

  constructor(
    readonly url: string,
    readonly protocols?: string | string[],
    readonly headers?: Headers,
  ) {
    super();
    FakeSocket.last = this;
  }

  send(data: string): void {
    if (this.readyState !== 1) {
      throw new Error("send on a socket that is not open");
    }
    this.sent.push(data);
  }

  close(code?: number, reason?: string): void {
    this.closedWith = { code, reason };
    if (this.readyState >= 2) {
      return;
    }
    this.readyState = 2;
    queueMicrotask(() => this.serverClose(code ?? 1005, reason ?? ""));
  }

  open(): void {
    this.readyState = 1;
    this.dispatchEvent(new Event("open"));
  }

  message(data: unknown): void {
    this.dispatchEvent(Object.assign(new Event("message"), { data }));
  }

  serverClose(code: number, reason = "", wasClean = code !== 1006): void {
    if (this.readyState === 3) {
      return;
    }
    this.readyState = 3;
    this.dispatchEvent(Object.assign(new Event("close"), { code, reason, wasClean }));
  }
}

const spec: EndpointSpec = {
  method: "GET",
  path: "/things/{id}/ws",
  pathParams: { id: "id" },
  queryParams: { verbose: "verbose" },
  headerParams: { trace: "X-Trace-Id" },
  accept: "",
  security: [
    { scheme: "api_key", kind: "apiKey", in: "query", name: "key" },
    { scheme: "jwt", kind: "bearer", in: "header", name: "Authorization" },
  ],
};

const Tick = z.object({ n: z.int(), at: dateTime().optional() });
const Note = z.object({ text: z.string().min(1), at: dateTime().optional() });

const bidi: WebSocketMapping<typeof Tick, typeof Note> = { recv: Tick, send: Note, single: false };
const serverOnly: WebSocketMapping<typeof Tick, null> = { recv: Tick, send: null, single: false };
const clientStream: WebSocketMapping<typeof Tick, typeof Note> = { recv: Tick, send: Note, single: true };

function client(options: Partial<ClientOptions> = {}) {
  return createClient({
    baseURL: "http://toy.test/api",
    webSocket: (url, protocols, init) => new FakeSocket(url, protocols, init.headers) as unknown as WebSocket,
    ...options,
  });
}

/** connect starts a connection, opens the fake socket and returns both. */
async function connect<R extends z.ZodType | null, S extends z.ZodType | null>(
  mapping: WebSocketMapping<R, S>,
  options?: WebSocketOptions,
  clientOptions?: Partial<ClientOptions>,
) {
  const pending = connectWebSocket(client(clientOptions), spec, { id: "a b", verbose: true }, options, mapping);
  const socket = await nextSocket();
  socket.open();
  const result = await pending;
  if (!result.ok) {
    throw result.error;
  }
  return { stream: result.value, socket, result };
}

/** nextSocket waits for the factory to run (credentials resolve asynchronously). */
async function nextSocket(): Promise<FakeSocket> {
  FakeSocket.last = undefined;
  for (let i = 0; i < 20 && !FakeSocket.last; i++) {
    await Promise.resolve();
  }
  if (!FakeSocket.last) {
    throw new Error("no socket was created");
  }
  return FakeSocket.last;
}

async function collect<T>(stream: AsyncIterable<T>): Promise<T[]> {
  const out: T[] = [];
  for await (const v of stream) {
    out.push(v);
  }
  return out;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("connectWebSocket", () => {
  it("builds a ws: URL with path, query and query credentials, and hands headers to the factory", async () => {
    const { socket, result } = await connect(
      serverOnly,
      { headers: { "X-Call": "1" }, protocols: ["v1"] },
      { auth: { api_key: "k&1", jwt: "tok" }, headers: { "X-Client": "web" } },
    );
    expect(socket.url).toBe("ws://toy.test/api/things/a%20b/ws?verbose=true&key=k%261");
    expect(socket.protocols).toEqual(["v1"]);
    expect(socket.headers?.get("authorization")).toBe("Bearer tok");
    expect(socket.headers?.get("x-client")).toBe("web");
    expect(socket.headers?.get("x-call")).toBe("1");
    expect(socket.headers?.has("accept")).toBe(false);
    expect(result.response.status).toBe(200);
  });

  it("sends header parameters to the factory only", async () => {
    const pending = connectWebSocket(client(), spec, { id: "x", trace: "t-1" }, undefined, serverOnly);
    const socket = await nextSocket();
    socket.open();
    expect((await pending).ok).toBe(true);
    expect(socket.url).toBe("ws://toy.test/api/things/x/ws");
    expect(socket.headers?.get("x-trace-id")).toBe("t-1");
  });

  it("maps https to wss and resolves a relative base URL against the page location", () => {
    expect(webSocketURL("https://toy.test/a?b=1")).toBe("wss://toy.test/a?b=1");
    expect(webSocketURL("ws://toy.test/a")).toBe("ws://toy.test/a");
    vi.stubGlobal("location", { href: "https://app.test/ui/page" });
    expect(webSocketURL("/api/things")).toBe("wss://app.test/api/things");
  });

  it("fails with a NetworkError on a relative URL outside a browser", async () => {
    const result = await connectWebSocket(client({ baseURL: "/api" }), spec, { id: "x" }, undefined, serverOnly);
    expect(result.ok).toBe(false);
    expect(result.error).toBeInstanceOf(NetworkError);
  });

  it("uses the global WebSocket by default", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    const pending = connectWebSocket(
      createClient({ baseURL: "http://toy.test" }),
      spec,
      { id: "x" },
      undefined,
      serverOnly,
    );
    const socket = await nextSocket();
    expect(socket.url).toBe("ws://toy.test/things/x/ws");
    socket.open();
    expect((await pending).ok).toBe(true);
  });

  it("reports a refused handshake as a NetworkError with the close code", async () => {
    const pending = connectWebSocket(client(), spec, { id: "missing" }, undefined, serverOnly);
    const socket = await nextSocket();
    socket.dispatchEvent(new Event("error"));
    socket.serverClose(1006);
    const result = await pending;
    expect(result.ok).toBe(false);
    const e = result.error as NetworkError;
    expect(e).toBeInstanceOf(NetworkError);
    expect(e.closeCode).toBe(1006);
    expect(e.closeReason).toBe("");
    expect(e.message).not.toContain("?");
    expect(result.response).toBeUndefined();
  });

  it("times out while waiting for the socket to open and closes it", async () => {
    vi.useFakeTimers();
    const pending = connectWebSocket(client({ timeoutMs: 50 }), spec, { id: "x" }, undefined, serverOnly);
    const socket = await nextSocket();
    await vi.advanceTimersByTimeAsync(60);
    const result = await pending;
    expect(result.error).toBeInstanceOf(TimeoutError);
    expect(socket.closedWith?.code).toBe(1000);
  });

  it("does not time out an open socket", async () => {
    vi.useFakeTimers();
    const { stream, socket } = await connect(serverOnly, { timeoutMs: 50 });
    await vi.advanceTimersByTimeAsync(200);
    expect(socket.closedWith).toBeUndefined();
    socket.message('{"n":1}');
    socket.serverClose(1000);
    expect(await collect(stream)).toEqual([{ n: 1 }]);
  });

  it("aborts before the socket opens", async () => {
    const controller = new AbortController();
    const pending = connectWebSocket(client(), spec, { id: "x" }, { signal: controller.signal }, serverOnly);
    const socket = await nextSocket();
    controller.abort();
    const result = await pending;
    expect(result.error).toBeInstanceOf(AbortError);
    expect(socket.closedWith?.code).toBe(1000);
  });

  it("does not connect with an aborted signal", async () => {
    const result = await connectWebSocket(client(), spec, { id: "x" }, { signal: AbortSignal.abort() }, serverOnly);
    expect(result.error).toBeInstanceOf(AbortError);
  });

  it("reports a failing credential provider as a NetworkError", async () => {
    const auth = () => {
      throw new Error("no token");
    };
    const result = await connectWebSocket(client({ auth }), spec, { id: "x" }, undefined, serverOnly);
    expect(result.error).toBeInstanceOf(NetworkError);
  });

  it("is typed by its mapping", () => {
    type Bidi = Awaited<ReturnType<typeof connectWebSocket<typeof Tick, typeof Note>>>;
    expectTypeOf<Bidi>().toEqualTypeOf<
      CallResult<WebSocketStream<z.output<typeof Tick>, z.output<typeof Note>>, TransportError>
    >();
    type ServerOnly = Awaited<ReturnType<typeof connectWebSocket<typeof Tick, null>>>;
    expectTypeOf<ServerOnly>().toEqualTypeOf<CallResult<WebSocketStream<z.output<typeof Tick>, never>, TransportError>>();
  });
});

describe("WebSocketStream", () => {
  it("yields decoded messages and ends when the server closes with 1000", async () => {
    const { stream, socket } = await connect(serverOnly);
    socket.message('{"n":1,"at":"2026-01-02T03:04:05Z"}');
    socket.message('{"n":2}');
    socket.serverClose(1000, "done");
    expect(await collect(stream)).toEqual([{ n: 1, at: new Date("2026-01-02T03:04:05Z") }, { n: 2 }]);
    expect(await stream.closed).toEqual({ code: 1000, reason: "done", wasClean: true });
  });

  it("yields messages that arrive while the consumer waits", async () => {
    const { stream, socket } = await connect(serverOnly);
    const iterator = stream[Symbol.asyncIterator]();
    const first = iterator.next();
    socket.message('{"n":7}');
    expect(await first).toEqual({ done: false, value: { n: 7 } });
    const end = iterator.next();
    socket.serverClose(1000);
    expect(await end).toEqual({ done: true, value: undefined });
  });

  it("throws a NetworkError with the code and reason on any other close, after the queued messages", async () => {
    const { stream, socket } = await connect(serverOnly);
    socket.message('{"n":1}');
    socket.serverClose(4001, "kicked");
    const iterator = stream[Symbol.asyncIterator]();
    expect(await iterator.next()).toEqual({ done: false, value: { n: 1 } });
    const failure = await iterator.next().catch((e: unknown) => e);
    expect(failure).toBeInstanceOf(NetworkError);
    expect(failure).toMatchObject({ closeCode: 4001, closeReason: "kicked" });
    expect(await iterator.next()).toEqual({ done: true, value: undefined });
  });

  it("treats a dropped connection as a failure", async () => {
    const { stream, socket } = await connect(serverOnly);
    socket.serverClose(1006);
    await expect(collect(stream)).rejects.toMatchObject({ kind: "network", closeCode: 1006 });
  });

  it("throws ResponseValidationError on an invalid message and closes the socket", async () => {
    const { stream, socket } = await connect(serverOnly);
    socket.message('{"n":"one"}');
    socket.message('{"n":2}');
    await expect(collect(stream)).rejects.toBeInstanceOf(ResponseValidationError);
    expect(socket.closedWith?.code).toBe(1000);
    expect(await collect(stream)).toEqual([]);
  });

  it("throws ResponseValidationError on a frame that is not JSON", async () => {
    const { stream, socket } = await connect(serverOnly);
    socket.message("not json");
    await expect(collect(stream)).rejects.toBeInstanceOf(ResponseValidationError);
  });

  it("rejects binary frames", async () => {
    const { stream, socket } = await connect(serverOnly);
    socket.message(new ArrayBuffer(2));
    await expect(collect(stream)).rejects.toBeInstanceOf(NetworkError);
    expect(socket.closedWith?.code).toBe(1000);
  });

  it("sends messages encoded through the schema as JSON text", async () => {
    const { stream, socket } = await connect(bidi);
    expect(stream.send({ text: "hi", at: new Date("2026-01-02T03:04:05Z") })).toEqual({ ok: true, value: undefined });
    expect(socket.sent).toEqual(['{"text":"hi","at":"2026-01-02T03:04:05.000Z"}']);
  });

  it("returns the validation error of an invalid message and sends nothing", async () => {
    const { stream, socket } = await connect(bidi);
    const result = stream.send({ text: "" });
    expect(result.ok).toBe(false);
    expect(result.error).toBeInstanceOf(RequestValidationError);
    expect(socket.sent).toEqual([]);
  });

  it("returns a NetworkError when sending on a closed socket", async () => {
    const { stream, socket } = await connect(bidi);
    socket.serverClose(1000);
    await stream.closed;
    const result = stream.send({ text: "late" });
    expect(result.error).toBeInstanceOf(NetworkError);
    expect(result.error).toMatchObject({ closeCode: 1000 });
  });

  it("end() sends goa's null terminator and keeps receiving", async () => {
    const { stream, socket } = await connect(bidi);
    stream.send({ text: "a" });
    stream.end();
    expect(socket.sent).toEqual(['{"text":"a"}', "null"]);
    expect(socket.closedWith).toBeUndefined();
    socket.message('{"n":1}');
    socket.serverClose(1000);
    expect(await collect(stream)).toEqual([{ n: 1 }]);
  });

  it("ends a client stream after its one result, without waiting for a clean close", async () => {
    const { stream, socket } = await connect(clientStream);
    stream.send({ text: "a" });
    stream.end();
    socket.message('{"n":3}');
    // goa's server drops the connection right after the result.
    socket.serverClose(1006);
    expect(await collect(stream)).toEqual([{ n: 3 }]);
  });

  it("fails a client stream whose connection drops before the result", async () => {
    const { stream, socket } = await connect(clientStream);
    stream.end();
    socket.serverClose(1006);
    await expect(collect(stream)).rejects.toBeInstanceOf(NetworkError);
  });

  it("closes the socket with 1000 and ends quietly when the consumer breaks out", async () => {
    const { stream, socket } = await connect(serverOnly);
    socket.message('{"n":1}');
    socket.message('{"n":2}');
    for await (const tick of stream) {
      expect(tick).toEqual({ n: 1 });
      break;
    }
    expect(socket.closedWith?.code).toBe(1000);
    expect(await stream.closed).toMatchObject({ code: 1000 });
    expect(await collect(stream)).toEqual([]);
  });

  it("ends a pending iteration on close(), whatever code the close carries", async () => {
    const { stream, socket } = await connect(serverOnly);
    const iterator = stream[Symbol.asyncIterator]();
    const pending = iterator.next();
    stream.close(4000, "bye");
    expect(await pending).toEqual({ done: true, value: undefined });
    expect(socket.closedWith).toEqual({ code: 4000, reason: "bye" });
    expect(await stream.closed).toEqual({ code: 4000, reason: "bye", wasClean: true });
  });

  it("throws AbortError when the signal aborts mid-stream, then ends", async () => {
    const controller = new AbortController();
    const { stream, socket } = await connect(serverOnly, { signal: controller.signal });
    const iterator = stream[Symbol.asyncIterator]();
    socket.message('{"n":1}');
    expect(await iterator.next()).toEqual({ done: false, value: { n: 1 } });
    const pending = iterator.next();
    controller.abort();
    await expect(pending).rejects.toBeInstanceOf(AbortError);
    expect(socket.closedWith?.code).toBe(1000);
    expect(await iterator.next()).toEqual({ done: true, value: undefined });
  });

  it("ignores an abort after the stream ended", async () => {
    const controller = new AbortController();
    const { stream, socket } = await connect(serverOnly, { signal: controller.signal });
    socket.serverClose(1000);
    expect(await collect(stream)).toEqual([]);
    controller.abort();
    expect(await collect(stream)).toEqual([]);
  });

  it("does not let a server-only stream send", async () => {
    const { stream } = await connect(serverOnly);
    // @ts-expect-error: the method streams no payload.
    const result = stream.send({ text: "x" });
    expect(result.error).toBeInstanceOf(NetworkError);
  });

  it("rejects messages when the method declares no result", async () => {
    const { stream, socket } = await connect({ recv: null, send: Note, single: false });
    expectTypeOf(stream).toEqualTypeOf<WebSocketStream<never, z.output<typeof Note>>>();
    socket.message("{}");
    await expect(collect(stream)).rejects.toBeInstanceOf(NetworkError);
  });
});
