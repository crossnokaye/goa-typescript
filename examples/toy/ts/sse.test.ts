import {
  AbortError,
  type GoaError,
  isGoaError,
  ResponseValidationError,
  type Thing,
  type ThingEvent,
  toy,
  unwrap,
  unwrapAsync,
} from "toy-client";
import { describe, expect, expectTypeOf, it } from "vitest";

import { client, collect } from "./helpers.js";

describe("stream_things_sse: a named type as the whole event", () => {
  it("yields three events, then ends when the server closes the stream", async () => {
    const stream = await unwrapAsync(toy.streamThingsSse(client(), { id: "abc" }));
    expect(stream.response.status).toBe(200);
    const events = await collect(stream);
    expect(events.map((e) => e.id)).toEqual(["abc-1", "abc-2", "abc-3"]);
    expect(events.map((e) => e.type)).toEqual(["thing", "thing", "thing"]);
    expectTypeOf(events).toEqualTypeOf<ThingEvent[]>();
  });

  it("decodes every type feature", async () => {
    const stream = await unwrapAsync(toy.streamThingsSse(client(), { id: "abc" }));
    const [first] = await collect(stream);
    const thing = first!.thing!;
    expectTypeOf(thing).toEqualTypeOf<Thing>();

    expect(thing.id).toBe("11111111-1111-4111-8111-111111111111");
    expect(thing.name).toBe("full-thing");
    expect(thing.kind).toBe("gadget");
    expect(thing.enabled).toBe(true);
    // date-time becomes a Date; the offset and the nanoseconds are understood.
    expect(thing.created_at).toBeInstanceOf(Date);
    expect(thing.created_at?.toISOString()).toBe("2026-01-02T10:04:05.123Z");
    // date stays a string.
    expect(thing.born_on).toBe("2026-01-02");
    expect(thing.count).toBe(Number.MAX_SAFE_INTEGER);
    expect(thing.small).toBe(-7);
    expect(thing.ratio).toBe(0.25);
    expect(thing.weight).toBe(1.5);
    // bytes arrive as base64 and become a Uint8Array.
    expect(thing.blob).toEqual(new Uint8Array([0x00, 0xff, 0x68, 0x69]));
    expect(thing.payload).toEqual({ nested: [1, "two", true, null] });
    expect(thing.tags).toEqual(["a", "b"]);
    expect(thing.attrs).toEqual({ x: 1, y: 2 });
    expect(thing.labels).toEqual([{ key: "env", value: "test" }]);
    // A union is the envelope goa serializes.
    expect(thing.shape).toEqual({ type: "circle", value: { radius: 2.5 } });
    // The JSON key set by Meta("struct:tag:json") is the property name.
    expect(thing.legacyName).toBe("old-name");

    // The type refers to itself.
    const parent = thing.parent!;
    expectTypeOf(parent).toEqualTypeOf<Thing>();
    expect(parent.id).toBe("22222222-2222-4222-8222-222222222222");
    expect(parent.created_at).toEqual(new Date("2025-12-31T23:59:59Z"));
    expect(parent.shape).toEqual({ type: "square", value: { side: 3 } });
    expect(parent.parent).toBeUndefined();
  });

  it("leaves absent attributes undefined, including the ones goa sends as null", async () => {
    const stream = await unwrapAsync(toy.streamThingsSse(client(), { id: "abc" }));
    const [, second, third] = await collect(stream);
    const minimal = second!.thing!;
    expect(minimal).toEqual({ id: "33333333-3333-4333-8333-333333333333", kind: "widget", enabled: true });
    expect(minimal.legacyName).toBeUndefined();
    expect(third!.thing!.shape).toEqual({ type: "square", value: { side: 4 } });
  });

  it("narrows a union on its discriminator", async () => {
    const stream = await unwrapAsync(toy.streamThingsSse(client(), { id: "abc" }));
    const sizes: number[] = [];
    for await (const { thing } of stream) {
      const shape = thing?.shape;
      if (shape?.type === "circle") {
        expectTypeOf(shape.value).toEqualTypeOf<{ radius: number }>();
        sizes.push(shape.value.radius);
      } else if (shape?.type === "square") {
        sizes.push(shape.value.side);
      }
    }
    expect(sizes).toEqual([2.5, 4]);
  });

  it("returns a typed goa error when the server refuses the stream", async () => {
    const result = await toy.streamThingsSse(client(), { id: "missing" });
    expect(result.ok).toBe(false);
    const error = result.error!;
    expectTypeOf(error).toEqualTypeOf<toy.StreamThingsSseError>();
    if (!isGoaError(error)) throw error;
    expect(error.kind).toBe("goa");
    expect(error.name).toBe("not_found");
    expect(error.status).toBe(404);
    expect(error.message).toBe('thing "missing" not found');
    expect(error.id).not.toBe("");
    expect(error.fault).toBe(false);
    expect(error.headers.get("goa-error")).toBe("not_found");

    // The design's errors are members of the endpoint's error union.
    expectTypeOf<GoaError<"not_found">>().toMatchTypeOf<toy.StreamThingsSseError>();
    expectTypeOf<GoaError<"bad_upstream">>().toMatchTypeOf<toy.StreamThingsSseError>();
    expect(toy.streamThingsSseErrors[404]).toEqual([{ name: "not_found" }]);
  });

  it("returns goa's own validation errors under their name", async () => {
    // The request passes the client's schema; only the server knows better.
    const rogue = client({
      middleware: [
        (req, next) => {
          const url = new URL(req.url);
          url.searchParams.set("delay_ms", "soon");
          return next(new Request(url, req));
        },
      ],
    });
    const result = await toy.streamThingsSse(rogue, { id: "abc" });
    const error = result.error!;
    if (!isGoaError(error)) throw error;
    expect(error.status).toBe(400);
    expect(error.name).toBe("invalid_field_type");
    expect(error.message).toContain("delay_ms");
  });

  it("throws AbortError from the iteration when the signal aborts mid-stream", async () => {
    const controller = new AbortController();
    const stream = await unwrapAsync(
      toy.streamThingsSse(client(), { id: "abc", delay_ms: 2_000 }, { signal: controller.signal }),
    );
    const seen: string[] = [];
    const consume = (async () => {
      for await (const event of stream) {
        seen.push(event.id);
        controller.abort();
      }
    })();
    await expect(consume).rejects.toBeInstanceOf(AbortError);
    expect(seen).toEqual(["abc-1"]);
  });

  it("returns AbortError when the signal aborts before the response", async () => {
    const result = await toy.streamThingsSse(client(), { id: "abc" }, { signal: AbortSignal.abort() });
    expect(result.error).toBeInstanceOf(AbortError);
  });

  it("stops the server's stream when the consumer breaks out", async () => {
    const stream = await unwrapAsync(toy.streamThingsSse(client(), { id: "abc", delay_ms: 2_000 }));
    const started = Date.now();
    for await (const event of stream) {
      expect(event.id).toBe("abc-1");
      break;
    }
    expect(await collect(stream)).toEqual([]);
    expect(Date.now() - started).toBeLessThan(1_500);
  });

  it("does not let a header timeout cut the stream short", async () => {
    const stream = await unwrapAsync(toy.streamThingsSse(client({ timeoutMs: 150 }), { id: "abc", delay_ms: 200 }));
    expect((await collect(stream)).length).toBe(3);
  });
});

describe("stream_wrapped_sse: attributes mapped onto SSE fields", () => {
  it("exposes the data, id, event and retry of every event", async () => {
    const stream = await unwrapAsync(toy.streamWrappedSse(client(), { id: "abc" }));
    const events = await collect(stream);
    expect(events.map((e) => e.id)).toEqual(["1", "2", "3"]);
    expect(events.map((e) => e.event)).toEqual(["thing", "thing", "thing"]);
    expect(events.map((e) => e.retry)).toEqual([1500, undefined, undefined]);
    expect(events.map((e) => e.data.name)).toEqual(["abc-1", "abc-2", "abc-3"]);
    expect(stream.lastEventId).toBe("3");
    expect(stream.retry).toBe(1500);
  });

  it("sends query parameters, encoding a Date as RFC 3339", async () => {
    const since = new Date("2026-03-04T05:06:07.890Z");
    const stream = await unwrapAsync(toy.streamWrappedSse(client(), { id: "abc", count: 2, since }));
    const events = await collect(stream);
    expect(events.length).toBe(2);
    expect(events[0]!.data.created_at).toEqual(since);
  });

  it("resumes after options.lastEventId", async () => {
    const first = await unwrapAsync(toy.streamWrappedSse(client(), { id: "abc", count: 5 }));
    let resumeFrom: string | undefined;
    for await (const event of first) {
      if (event.id === "2") {
        resumeFrom = first.lastEventId;
        break;
      }
    }
    expect(resumeFrom).toBe("2");

    const second = await unwrapAsync(
      toy.streamWrappedSse(client(), { id: "abc", count: 5 }, { lastEventId: resumeFrom! }),
    );
    expect((await collect(second)).map((e) => e.id)).toEqual(["3", "4", "5"]);
  });

  it("leaves the Last-Event-ID attribute out of the request type", () => {
    expectTypeOf<toy.StreamWrappedSsePayload>().toEqualTypeOf<{
      id: string;
      count?: number | undefined;
      since?: Date | undefined;
    }>();
  });

  it("maps a declared error raised before the first event", async () => {
    const result = await toy.streamWrappedSse(client(), { id: "abc" }, { lastEventId: "not-a-number" });
    const error = result.error!;
    if (!isGoaError(error)) throw error;
    expect(error.name).toBe("precondition_failed");
    expect(error.status).toBe(400);
  });
});

describe("stream_post_sse: a JSON request body", () => {
  it("sends the filter as the body", async () => {
    const since = new Date("2026-01-01T00:00:00Z");
    const stream = await unwrapAsync(toy.streamPostSse(client(), { filter: { kinds: ["gadget", "widget"], since } }));
    const events = await collect(stream);
    expect(events.map((e) => e.thing?.kind)).toEqual(["gadget", "widget"]);
    expect(events.map((e) => e.type)).toEqual(["filtered", "filtered"]);
    expect(events[0]!.thing?.created_at).toEqual(since);
  });

  it("ends at once when the server has nothing to send", async () => {
    const stream = await unwrapAsync(toy.streamPostSse(client(), { filter: {} }));
    expect(await collect(stream)).toEqual([]);
    expect(stream.lastEventId).toBeUndefined();
  });
});

describe("primitive events", () => {
  it("joins multi-line text data with a line feed", async () => {
    const stream = await unwrapAsync(toy.streamTextSse(client()));
    const lines = await collect(stream);
    expect(lines).toEqual(["line one\nline two", "single"]);
    expectTypeOf(lines).toEqualTypeOf<string[]>();
  });

  it("parses integers", async () => {
    const stream = await unwrapAsync(toy.streamCountSse(client()));
    const counts = await collect(stream);
    expect(counts).toEqual([1, 2, 3]);
    expectTypeOf(counts).toEqualTypeOf<number[]>();
  });
});

describe("stream_viewed_sse: a result type with views", () => {
  it("makes attributes missing from a view optional", async () => {
    const defaults = await collect(await unwrapAsync(toy.streamViewedSse(client(), {})));
    expect(defaults).toEqual([{ id: "viewed", name: "viewed-thing" }]);
    const extended = await collect(await unwrapAsync(toy.streamViewedSse(client(), { view: "extended" })));
    expect(extended).toEqual([{ id: "viewed", name: "viewed-thing", secret: "s3cr3t" }]);
    expectTypeOf(extended[0]!.secret).toEqualTypeOf<string | undefined>();
  });
});

describe("unwrap", () => {
  it("throws the error of a failed call", async () => {
    const result = await toy.streamThingsSse(client(), { id: "missing" });
    expect(() => unwrap(result)).toThrow('thing "missing" not found');
    await expect(unwrapAsync(toy.streamThingsSse(client(), { id: "missing" }))).rejects.toMatchObject({
      kind: "goa",
      name: "not_found",
    });
  });
});

describe("a server that breaks the contract", () => {
  it("throws ResponseValidationError from the iteration", async () => {
    // Rewrite the first event so it no longer matches the schema.
    const tamper = client({
      middleware: [
        async (req, next) => {
          const res = await next(req);
          const body = (await res.text()).replace('"id":"abc-1"', '"id":42');
          return new Response(body, { status: res.status, headers: res.headers });
        },
      ],
    });
    const stream = await unwrapAsync(toy.streamThingsSse(tamper, { id: "abc" }));
    const consume = collect(stream);
    await expect(consume).rejects.toBeInstanceOf(ResponseValidationError);
    await expect(consume).rejects.toMatchObject({ kind: "response_validation" });
  });
});
