import {
  type CallResult,
  createClient,
  type GoaError,
  isGoaError,
  type Middleware,
  RequestValidationError,
  type Thing,
  type ThingReply,
  type ThingSummary,
  type ThingWithViews,
  toy,
  UnexpectedResponseError,
  unwrap,
  unwrapAsync,
  type ValidationProblem,
} from "toy-client";
import { describe, expect, expectTypeOf, it } from "vitest";

import { baseURL, client, collect } from "./helpers.js";

/** spy records every request the client sends. */
function spy(): { middleware: Middleware; requests: Request[] } {
  const requests: Request[] = [];
  return {
    requests,
    middleware: (req, next) => {
      requests.push(req.clone());
      return next(req);
    },
  };
}

describe("get_thing: a named result with a response header", () => {
  it("returns the bare value, with the response beside it", async () => {
    const result = await toy.getThing(client(), { id: "abc" });
    expectTypeOf(result).toEqualTypeOf<CallResult<ThingReply, toy.GetThingError>>();
    if (!result.ok) throw result.error;
    expectTypeOf(result.value).toEqualTypeOf<ThingReply>();
    expect(result.value.thing.name).toBe("full-thing");
    expect(result.response.status).toBe(200);
    expect(result.response.headers.get("content-type")).toBe("application/json");
  });

  it("decodes every type feature of the body", async () => {
    const { thing } = await unwrapAsync(toy.getThing(client(), { id: "abc" }));
    expectTypeOf(thing).toEqualTypeOf<Thing>();
    expect(thing.created_at?.toISOString()).toBe("2026-01-02T10:04:05.123Z");
    expect(thing.born_on).toBe("2026-01-02");
    expect(thing.blob).toEqual(new Uint8Array([0x00, 0xff, 0x68, 0x69]));
    expect(thing.shape).toEqual({ type: "circle", value: { radius: 2.5 } });
    expect(thing.legacyName).toBe("old-name");
    expect(thing.parent?.created_at).toEqual(new Date("2025-12-31T23:59:59Z"));
  });

  it("fills an attribute from the header the design maps it to", async () => {
    const result = await toy.getThing(client(), { id: "abc" });
    const reply = unwrap(result);
    expect(reply.etag).toBe('"abc-v1"');
    expect(result.response?.headers.get("etag")).toBe('"abc-v1"');
  });

  it("sends path, array, boolean and date-time parameters and a header", async () => {
    const seen = spy();
    const since = new Date("2026-03-04T05:06:07.000Z");
    const reply = await unwrapAsync(
      toy.getThing(client({ middleware: [seen.middleware] }), {
        id: "a b/c",
        tags: ["x", "y z"],
        verbose: true,
        since,
        trace: "trace-1",
      }),
    );
    const url = new URL(seen.requests[0]!.url);
    expect(url.pathname).toBe("/things/a%20b%2Fc");
    expect(url.searchParams.getAll("tag")).toEqual(["x", "y z"]);
    expect(seen.requests[0]!.headers.get("x-trace-id")).toBe("trace-1");
    expect(seen.requests[0]!.headers.get("accept")).toBe("application/json");
    // The server echoes what it decoded; the identifier comes back in the ETag.
    expect(reply.etag).toBe('"a b/c-v1"');
    expect(reply.tags).toEqual(["x", "y z"]);
    expect(reply.verbose).toBe(true);
    expect(reply.since).toEqual(since);
    expect(reply.trace).toBe("trace-1");
  });

  it("returns a declared error by name, with the response", async () => {
    const result = await toy.getThing(client(), { id: "missing" });
    expect(result.ok).toBe(false);
    expect(result.response?.status).toBe(404);
    if (!isGoaError(result.error, "not_found")) throw result.error;
    expectTypeOf(result.error).toEqualTypeOf<GoaError<"not_found">>();
    expect(result.error.message).toBe('thing "missing" not found');
    expect(result.error.response).toBe(result.response);
  });

  it("has no response when the request never left", async () => {
    const result = await toy.getThing(client(), { id: "abc", since: new Date(Number.NaN) });
    expect(result.error).toBeInstanceOf(RequestValidationError);
    expect(result.response).toBeUndefined();
  });

  it("throws the error from unwrapAsync", async () => {
    await expect(unwrapAsync(toy.getThing(client(), { id: "missing" }))).rejects.toMatchObject({
      kind: "goa",
      name: "not_found",
    });
  });

  it("reports a 404 that goa did not produce as unexpected", async () => {
    const result = await toy.getThing(createClient({ baseURL: `${baseURL}/nowhere` }), { id: "abc" });
    expect(result.error).toBeInstanceOf(UnexpectedResponseError);
    expect(result.response?.status).toBe(404);
  });
});

describe("views and collections", () => {
  it("leaves the attributes a view omits undefined", async () => {
    const viewed = await unwrapAsync(toy.getThingViewed(client(), {}));
    expectTypeOf(viewed).toEqualTypeOf<ThingWithViews>();
    expect(viewed).toEqual({ id: "viewed", name: "viewed-thing" });
    const extended = await toy.getThingViewed(client(), { view: "extended" });
    expect(unwrap(extended)).toEqual({ id: "viewed", name: "viewed-thing", secret: "s3cr3t" });
    expect(extended.response?.headers.get("goa-view")).toBe("extended");
  });

  it("returns CollectionOf as an array of the element type", async () => {
    const things = await unwrapAsync(toy.listThings(client()));
    expectTypeOf(things).toEqualTypeOf<ThingWithViews[]>();
    expectTypeOf<toy.ListThingsResult>().toEqualTypeOf<ThingWithViews[]>();
    expect(things).toEqual([
      { id: "one", name: "first" },
      { id: "two", name: "second" },
    ]);
  });
});

describe("create_thing: a JSON body, a 201 and custom errors", () => {
  it("posts the payload and decodes the created thing", async () => {
    const seen = spy();
    const result = await toy.createThing(client({ middleware: [seen.middleware] }), { name: "fresh", kind: "gadget" });
    expect(result.response?.status).toBe(201);
    expect(unwrap(result)).toMatchObject({ name: "fresh", kind: "gadget", enabled: true });
    expect(seen.requests[0]!.method).toBe("POST");
    expect(seen.requests[0]!.headers.get("content-type")).toBe("application/json");
    expect(await seen.requests[0]!.json()).toEqual({ name: "fresh", kind: "gadget" });
  });

  it("narrows a named error type and merges its header into the detail", async () => {
    const result = await toy.createThing(client(), { name: "bad" });
    expect(result.response?.status).toBe(422);
    if (!isGoaError(result.error, "invalid")) throw result.error;
    expectTypeOf(result.error.detail).toEqualTypeOf<ValidationProblem>();
    expect(result.error.detail).toEqual({ field: "name", message: "bad is not a good name", retry_after: 30 });
    expect(result.error.message).toBe("bad is not a good name");
    expect(result.error.headers.get("retry-after")).toBe("30");
  });

  it("decodes an error type the design does not name", async () => {
    const result = await toy.createThing(client(), { name: "taken" });
    if (!isGoaError(result.error, "conflict")) throw result.error;
    expectTypeOf(result.error.detail).toEqualTypeOf<toy.CreateThingConflictDetail>();
    expect(result.error.status).toBe(409);
    expect(result.error.detail).toEqual({
      existing_id: "11111111-1111-4111-8111-111111111111",
      since: new Date("2026-01-02T03:04:05Z"),
    });
  });

  it("validates the payload before sending it", async () => {
    const seen = spy();
    const result = await toy.createThing(client({ middleware: [seen.middleware] }), { name: "x", kind: "gizmo" as "widget" });
    expect(result.error).toBeInstanceOf(RequestValidationError);
    expect(seen.requests).toHaveLength(0);
  });
});

describe("several success statuses", () => {
  it("restores the tag attribute of the response the server chose", async () => {
    const updated = await toy.upsertThing(client(), { id: "old", name: "n" });
    expect(updated.response?.status).toBe(200);
    expect(unwrap(updated).outcome).toBe("updated");

    // The 201 body has no outcome: the client sets it from the tag.
    const created = await toy.upsertThing(client(), { id: "new", name: "n" });
    expect(created.response?.status).toBe(201);
    expect(unwrap(created).outcome).toBe("created");
    expect(unwrap(created).thing.name).toBe("n");
  });

  it("decodes a tagged 200 that carries one attribute and an untagged 204 with one type", async () => {
    const found = await toy.findThing(client(), { name: "needle" });
    expectTypeOf(unwrap(found)).toEqualTypeOf<toy.FindThingResult>();
    expect(found.response?.status).toBe(200);
    expect(unwrap(found)).toEqual({
      match: { id: "11111111-1111-4111-8111-111111111111", name: "needle", found_at: new Date("2026-01-02T03:04:05Z") },
      found: "yes",
    });

    const nothing = await toy.findThing(client(), { name: "nothing" });
    expect(nothing.response?.status).toBe(204);
    expect(unwrap(nothing)).toEqual({});
  });
});

describe("delete_thing: no content", () => {
  it("resolves to undefined", async () => {
    const result = await toy.deleteThing(client(), { id: "abc" });
    expectTypeOf(result).toEqualTypeOf<CallResult<void, toy.DeleteThingError>>();
    expect(result).toMatchObject({ ok: true, value: undefined });
    expect(result.response?.status).toBe(204);
    expect(await unwrapAsync(toy.deleteThing(client(), { id: "abc" }))).toBeUndefined();
  });

  it("still fails with a typed error", async () => {
    const result = await toy.deleteThing(client(), { id: "missing" });
    expect(isGoaError(result.error, "not_found")).toBe(true);
  });
});

describe("raw bodies", () => {
  it("leaves a download unread on the response, with the mapped headers as the value", async () => {
    const result = await toy.downloadThing(client(), { id: "abc" });
    const value = unwrap(result);
    expectTypeOf(value).toEqualTypeOf<{ content_length: number; content_type: string }>();
    expect(value).toEqual({ content_length: 16, content_type: "application/octet-stream" });
    expect(result.response?.bodyUsed).toBe(false);
    const bytes = new Uint8Array(await result.response!.arrayBuffer());
    expect(bytes.length).toBe(16);
    expect([...bytes.subarray(0, 2)]).toEqual([0x00, 0xff]);
    expect(new TextDecoder().decode(bytes.subarray(2))).toBe("content of abc");
  });

  it("decodes the error of a failed download", async () => {
    const result = await toy.downloadThing(client(), { id: "missing" });
    expect(isGoaError(result.error, "not_found")).toBe(true);
  });

  it("uploads a Blob as is, after the payload", async () => {
    expectTypeOf(toy.uploadThing).parameter(2).toEqualTypeOf<BodyInit>();
    const blob = new Blob(["hello, toy"], { type: "text/plain" });
    const uploaded = await unwrapAsync(toy.uploadThing(client(), { id: "abc" }, blob));
    expect(uploaded).toEqual({ size: 10, content_type: "text/plain", text: "hello, toy" });
  });

  it("lets a header parameter set the content type of an upload", async () => {
    const data = new TextEncoder().encode("bytes");
    const uploaded = await unwrapAsync(
      toy.uploadThing(client(), { id: "abc", content_type: "application/x-thing" }, data.buffer as ArrayBuffer),
    );
    expect(uploaded).toEqual({ size: 5, content_type: "application/x-thing", text: "bytes" });
  });
});

describe("stream_or_list: one method, two functions", () => {
  it("returns the plain result when asked for JSON", async () => {
    const seen = spy();
    const result = await toy.streamOrList(client({ middleware: [seen.middleware] }), {});
    // The server rejects any Accept header other than the two it expects.
    expect(seen.requests[0]!.headers.get("accept")).toBe("application/json");
    expect(result.response?.headers.get("content-type")).toBe("application/json");
    const things = unwrap(result);
    expectTypeOf(things).toEqualTypeOf<ThingSummary[]>();
    expect(things).toEqual([{ id: "one", created_at: new Date("2026-01-02T03:04:05Z") }, { id: "two" }]);
  });

  it("returns an empty array when there is nothing", async () => {
    expect(await unwrapAsync(toy.streamOrList(client(), { empty: true }))).toEqual([]);
  });

  it("streams the same things when asked for events", async () => {
    const seen = spy();
    const result = await toy.streamOrListStream(client({ middleware: [seen.middleware] }), {});
    expect(seen.requests[0]!.headers.get("accept")).toBe("text/event-stream");
    const stream = unwrap(result);
    // The response is on the result and on the stream.
    expect(result.response).toBe(stream.response);
    expect(result.response?.headers.get("content-type")).toMatch(/^text\/event-stream/);
    const things = await collect(stream);
    expectTypeOf(things).toEqualTypeOf<ThingSummary[]>();
    expect(things.map((t) => t.id)).toEqual(["one", "two"]);
  });

  it("shares the request type and the errors between the two", () => {
    expectTypeOf(toy.streamOrListStream).parameter(1).toEqualTypeOf<toy.StreamOrListPayload>();
    expectTypeOf(toy.streamOrList).parameter(1).toEqualTypeOf<toy.StreamOrListPayload>();
  });
});

describe("results that are not objects", () => {
  it("decodes a primitive", async () => {
    const count = await unwrapAsync(toy.thingCount(client(), {}));
    expectTypeOf(count).toEqualTypeOf<number>();
    expect(count).toBe(3);
  });

  it("decodes bytes from the base64 string goa writes", async () => {
    const blob = await unwrapAsync(toy.thingBlob(client()));
    expectTypeOf(blob).toEqualTypeOf<Uint8Array>();
    expect(blob).toEqual(new Uint8Array([0x00, 0xff, 0x68, 0x69]));
  });

  it("decodes an error that is a plain string", async () => {
    const result = await toy.thingCount(client(), { fail: true });
    if (!isGoaError(result.error, "teapot")) throw result.error;
    expectTypeOf(result.error.detail).toEqualTypeOf<string>();
    expect(result.error.status).toBe(418);
    expect(result.error.detail).toBe("short and stout");
  });
});

describe("SSE functions", () => {
  it("carry the response on the result too", async () => {
    const result = await toy.streamCountSse(client());
    expect(result.response?.status).toBe(200);
    expect(await collect(unwrap(result))).toEqual([1, 2, 3]);

    const failed = await toy.streamThingsSse(client(), { id: "missing" });
    expect(failed.response?.status).toBe(404);
    expect(isGoaError(failed.error, "not_found")).toBe(true);
  });
});
