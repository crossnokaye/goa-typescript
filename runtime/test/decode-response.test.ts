import { describe, expect, expectTypeOf, it } from "vitest";
import * as z from "zod";

import {
  AbortError,
  bytes,
  type CallResult,
  type ClientError,
  createClient,
  dateTime,
  decodeResponse,
  type EndpointSpec,
  GoaError,
  goaErrorDecoder,
  isGoaError,
  NetworkError,
  type OtherErrorName,
  parseHeader,
  ResponseValidationError,
  type ResponseTable,
  type Result,
  send,
  UnexpectedResponseError,
  unwrap,
  unwrapAsync,
} from "../src/index.js";

const Thing = z.object({
  id: z.string(),
  name: z.string().optional(),
  created_at: dateTime().optional(),
  etag: z.string().optional(),
  count: z.int().optional(),
  more: z.boolean().optional(),
  tags: z.array(z.string()).optional(),
  counts: z.array(z.int()).optional(),
  outcome: z.enum(["updated", "created", "accepted"]).optional(),
});

const errors = {
  404: [{ name: "not_found" }],
  422: [
    {
      name: "invalid",
      schema: z.object({ field: z.string(), message: z.string(), retry_after: z.int().optional() }),
      headers: { retry_after: { name: "Retry-After", shape: "number" } },
    },
  ],
  429: [
    {
      name: "throttled",
      schema: z.object({ retry_after: z.int() }),
      headers: { retry_after: { name: "Retry-After", shape: "number" } },
    },
  ],
} as const;
const decodeError = goaErrorDecoder(errors);

function json(status: number, body: unknown, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json", ...headers } });
}

describe("decodeResponse: body kinds", () => {
  it("decodes a JSON object of attributes", async () => {
    const res = json(200, { id: "a", created_at: "2026-01-02T03:04:05Z" });
    const result = await decodeResponse(res, Thing, { 200: { body: "fields" } }, decodeError);
    expect(unwrap(result)).toEqual({ id: "a", created_at: new Date("2026-01-02T03:04:05Z") });
    expect(result.response).toBe(res);
    expect(result.response?.status).toBe(200);
  });

  it("accepts a +json content type", async () => {
    const res = json(200, { id: "a" }, { "content-type": "application/vnd.thing+json; charset=utf-8" });
    expect(unwrap(await decodeResponse(res, Thing, { 200: { body: "fields" } }, decodeError))).toEqual({ id: "a" });
  });

  it("places an attribute body under its key", async () => {
    const Found = z.object({ diagram: z.object({ svg: z.string() }).optional(), found: z.string().optional() });
    const table = { 200: { body: "attr", attr: "diagram", tag: ["found", "yes"] }, 204: { body: "none" } } as const;
    expect(unwrap(await decodeResponse(json(200, { svg: "<svg/>" }), Found, table, decodeError))).toEqual({
      diagram: { svg: "<svg/>" },
      found: "yes",
    });
    // goa writes null for an unset attribute.
    expect(unwrap(await decodeResponse(json(200, null), Found, table, decodeError))).toEqual({ found: "yes" });
    expect(unwrap(await decodeResponse(new Response(null, { status: 204 }), Found, table, decodeError))).toEqual({});
  });

  it("takes a whole body as the value itself", async () => {
    const whole = { 200: { body: "whole", scalar: true } } as const;
    expect(unwrap(await decodeResponse(json(200, ["a", "b"]), z.array(z.string()), whole, decodeError))).toEqual(["a", "b"]);
    expect(unwrap(await decodeResponse(json(200, 3), z.int(), whole, decodeError))).toBe(3);
    expect(unwrap(await decodeResponse(json(200, "AP9oaQ=="), bytes(), whole, decodeError))).toEqual(
      new Uint8Array([0x00, 0xff, 0x68, 0x69]),
    );
  });

  it("reads the null of a nil slice or map as empty", async () => {
    const list = { 200: { body: "whole", scalar: true, nullAs: "array" } } as const;
    const map = { 200: { body: "whole", scalar: true, nullAs: "map" } } as const;
    expect(unwrap(await decodeResponse(json(200, null), z.array(z.string()), list, decodeError))).toEqual([]);
    expect(unwrap(await decodeResponse(json(200, null), z.record(z.string(), z.int()), map, decodeError))).toEqual({});
    const plain = await decodeResponse(json(200, null), z.string(), { 200: { body: "whole", scalar: true } }, decodeError);
    expect(plain.error).toBeInstanceOf(ResponseValidationError);
  });

  it("returns undefined for a method without result", async () => {
    const res = new Response(null, { status: 204 });
    const result = await decodeResponse(res, null, { 204: { body: "none" } }, decodeError);
    expect(result).toEqual({ ok: true, value: undefined, response: res });
    expectTypeOf(result).toEqualTypeOf<
      CallResult<void, Awaited<ReturnType<typeof decodeError>> | NetworkError | AbortError | TimeoutErrorOf>
    >();
  });

  it("leaves a raw body unread", async () => {
    const Download = z.object({ content_length: z.int() });
    const table = {
      200: { body: "raw", headers: { content_length: { name: "Content-Length", shape: "number" } } },
    } as const;
    const res = new Response("zip bytes", { status: 200, headers: { "content-length": "9", "content-type": "application/zip" } });
    const result = await decodeResponse(res, Download, table, decodeError);
    expect(unwrap(result)).toEqual({ content_length: 9 });
    expect(result.response?.bodyUsed).toBe(false);
    expect(await result.response?.text()).toBe("zip bytes");

    const bare = new Response("plain", { status: 200 });
    const none = await decodeResponse(bare, null, { 200: { body: "raw" } }, decodeError);
    expect(none.ok).toBe(true);
    expect(await bare.text()).toBe("plain");
  });
});

// The timeout error only comes out of send; this alias keeps the type test above readable.
type TimeoutErrorOf = Extract<ClientError, { kind: "timeout" }>;

describe("decodeResponse: headers and tags", () => {
  const table = {
    200: {
      body: "fields",
      headers: {
        etag: { name: "ETag", shape: "string" },
        count: { name: "X-Count", shape: "number" },
        more: { name: "X-More", shape: "boolean" },
        tags: { name: "X-Tags", shape: "string[]" },
        counts: { name: "X-Counts", shape: "number[]" },
        created_at: { name: "X-Created", shape: "string" },
      },
    },
    201: { body: "fields", tag: ["outcome", "created"] },
    202: { body: "none", tag: ["outcome", "accepted"] },
  } as const satisfies ResponseTable;

  it("parses each header by its shape and decodes it with the schema", async () => {
    const res = json(200, { id: "a" }, {
      etag: '"v1"',
      "x-count": "42",
      "x-more": "true",
      "x-tags": "a, b,c",
      "x-counts": "1, 2",
      "x-created": "2026-01-02T03:04:05Z",
    });
    expect(unwrap(await decodeResponse(res, Thing, table, decodeError))).toEqual({
      id: "a",
      etag: '"v1"',
      count: 42,
      more: true,
      tags: ["a", "b", "c"],
      counts: [1, 2],
      created_at: new Date("2026-01-02T03:04:05Z"),
    });
  });

  it("leaves the attribute of a missing header unset", async () => {
    expect(unwrap(await decodeResponse(json(200, { id: "a" }), Thing, table, decodeError))).toEqual({ id: "a" });
  });

  it("reports an unparseable number through the schema", async () => {
    const res = json(200, { id: "a" }, { "x-count": "many" });
    const result = await decodeResponse(res, Thing, table, decodeError);
    expect(result.ok).toBe(false);
    expect(result.error).toBeInstanceOf(ResponseValidationError);
    expect((result.error as ResponseValidationError).issues[0]?.path).toEqual(["count"]);
    expect(result.response).toBe(res);
  });

  it("parses header shapes", () => {
    expect(parseHeader("1.5", "number")).toBe(1.5);
    expect(parseHeader("", "number")).toBe("");
    expect(parseHeader("FALSE", "boolean")).toBe(false);
    expect(parseHeader("maybe", "boolean")).toBe("maybe");
    expect(parseHeader("", "string[]")).toEqual([]);
    expect(parseHeader("true, false", "boolean[]")).toEqual([true, false]);
  });

  it("restores the tag attribute", async () => {
    expect(unwrap(await decodeResponse(json(201, { id: "a" }), Thing, table, decodeError))).toEqual({
      id: "a",
      outcome: "created",
    });
    // A tagged response without body is built from the tag alone; id is required, so it fails.
    const accepted = await decodeResponse(new Response(null, { status: 202 }), Thing, table, decodeError);
    expect(accepted.error).toBeInstanceOf(ResponseValidationError);
    expect(accepted.response?.status).toBe(202);
  });

  it("takes a scalar result from its header", async () => {
    const scalar = { 200: { body: "none", scalar: true, headers: { "": { name: "X-Count", shape: "number" } } } } as const;
    const res = new Response(null, { status: 200, headers: { "x-count": "7" } });
    expect(unwrap(await decodeResponse(res, z.int(), scalar, decodeError))).toBe(7);
    const missing = await decodeResponse(new Response(null, { status: 200 }), z.int(), scalar, decodeError);
    expect(missing.error).toBeInstanceOf(ResponseValidationError);
  });
});

describe("decodeResponse: failures", () => {
  const table = { 200: { body: "fields" } } as const;

  it("hands an undeclared status to the error decoder", async () => {
    const res = json(404, { name: "not_found", id: "x", message: "no such thing" }, { "goa-error": "not_found" });
    const result = await decodeResponse(res, Thing, table, decodeError);
    expect(result.ok).toBe(false);
    expect(result.error).toMatchObject({ kind: "goa", name: "not_found", status: 404, message: "no such thing" });
    expect(result.response).toBe(res);

    const odd = await decodeResponse(new Response("nope", { status: 500 }), Thing, table, decodeError);
    expect(odd.error).toBeInstanceOf(UnexpectedResponseError);
    expect(odd.response?.status).toBe(500);
  });

  it("does not treat a success status outside the table as success", async () => {
    const result = await decodeResponse(json(201, { id: "a" }), Thing, table, decodeError);
    expect(result.error).toBeInstanceOf(UnexpectedResponseError);
  });

  it("rejects a declared status whose body is not JSON", async () => {
    const res = new Response("<html>login</html>", { status: 200, headers: { "content-type": "text/html" } });
    const result = await decodeResponse(res, Thing, table, decodeError);
    expect(result.error).toBeInstanceOf(UnexpectedResponseError);
    expect((result.error as UnexpectedResponseError).body).toBe("<html>login</html>");
    expect(result.response).toBe(res);
  });

  it("reports malformed JSON and schema violations with the response", async () => {
    const broken = new Response("{", { status: 200, headers: { "content-type": "application/json" } });
    const malformed = await decodeResponse(broken, Thing, table, decodeError);
    expect(malformed.error).toBeInstanceOf(ResponseValidationError);
    expect(malformed.response).toBe(broken);

    const res = json(200, { id: 1 });
    const invalid = await decodeResponse(res, Thing, table, decodeError);
    expect(invalid.error).toBeInstanceOf(ResponseValidationError);
    expect(invalid.response).toBe(res);
  });

  it("reports a body that cannot be read instead of throwing", async () => {
    const failing = (error: Error) =>
      new Response(new ReadableStream({ pull: (controller) => controller.error(error) }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    const dropped = await decodeResponse(failing(new TypeError("terminated")), Thing, table, decodeError);
    expect(dropped.error).toBeInstanceOf(NetworkError);
    expect(dropped.response?.status).toBe(200);
    const aborted = await decodeResponse(failing(new DOMException("aborted", "AbortError")), Thing, table, decodeError);
    expect(aborted.error).toBeInstanceOf(AbortError);
  });
});

describe("goaErrorDecoder: headers of a custom error", () => {
  it("merges a mapped header into the detail", async () => {
    const res = json(422, { field: "name", message: "too short" }, { "goa-error": "invalid", "retry-after": "30" });
    const e = await decodeError(res);
    if (!isGoaError(e, "invalid")) throw e;
    expect(e.detail).toEqual({ field: "name", message: "too short", retry_after: 30 });
    expect(e.message).toBe("too short");
  });

  it("leaves an absent header out", async () => {
    const e = await decodeError(json(422, { field: "name", message: "too short" }, { "goa-error": "invalid" }));
    if (!isGoaError(e, "invalid")) throw e;
    expect(e.detail).toEqual({ field: "name", message: "too short" });
  });

  it("builds the detail from headers alone when the body is empty", async () => {
    const res = new Response(null, { status: 429, headers: { "goa-error": "throttled", "retry-after": "5" } });
    const e = await decodeError(res);
    if (!isGoaError(e, "throttled")) throw e;
    expect(e.detail).toEqual({ retry_after: 5 });
  });
});

describe("CallResult", () => {
  it("is assignable to Result, so unwrap returns the bare value", async () => {
    const call = decodeResponse(json(200, { id: "a" }), Thing, { 200: { body: "fields" } }, decodeError);
    expectTypeOf<CallResult<string, Error>>().toExtend<Result<string, Error>>();
    // A failure that never saw a response (encode, send) is returned as is.
    expectTypeOf<Extract<Result<number, Error>, { ok: false }>>().toExtend<CallResult<string, Error>>();
    const value = await unwrapAsync(call);
    expectTypeOf(value).toEqualTypeOf<z.output<typeof Thing>>();
    expect(value.id).toBe("a");
  });

  it("requires the response on success only", () => {
    expectTypeOf<Extract<CallResult<string, Error>, { ok: true }>["response"]>().toEqualTypeOf<Response>();
    expectTypeOf<Extract<CallResult<string, Error>, { ok: false }>["response"]>().toEqualTypeOf<Response | undefined>();
  });
});

describe("isGoaError with a name", () => {
  type Problem = { field: string };
  type CreateError = GoaError<"invalid", Problem> | GoaError<"not_found"> | GoaError<OtherErrorName> | ClientError;
  const response = new Response(null, { status: 422 });
  const invalid = new GoaError({ name: "invalid" as const, message: "bad", body: {}, response, detail: { field: "name" } });

  it("matches on the name at run time", () => {
    expect(isGoaError(invalid, "invalid")).toBe(true);
    expect(isGoaError(invalid, "not_found")).toBe(false);
    expect(isGoaError(new NetworkError("down"), "NetworkError")).toBe(false);
    expect(isGoaError(undefined, "invalid")).toBe(false);
  });

  it("narrows an endpoint's error union to the declared member", () => {
    const e = invalid as CreateError;
    if (isGoaError(e, "invalid")) {
      expectTypeOf(e).toEqualTypeOf<GoaError<"invalid", Problem>>();
      expectTypeOf(e.detail).toEqualTypeOf<Problem>();
    }
    if (isGoaError(e, "not_found")) {
      expectTypeOf(e.detail).toEqualTypeOf<undefined>();
    }
    // Comparing the name does not get that far.
    if (isGoaError(e) && e.name === "invalid") {
      expectTypeOf(e.detail).toEqualTypeOf<Problem | undefined>();
    }
    // result.error is accepted before the result is narrowed.
    const result = { ok: false, error: e } as Result<string, CreateError>;
    if (isGoaError(result.error, "invalid")) {
      expectTypeOf(result.error.detail).toEqualTypeOf<Problem>();
    }
  });

  it("cannot narrow the detail of an unknown value, as in a catch block", () => {
    try {
      throw invalid;
    } catch (e) {
      if (isGoaError(e, "invalid")) {
        expectTypeOf(e).toEqualTypeOf<GoaError<"invalid", unknown>>();
        expectTypeOf(e.detail).toEqualTypeOf<unknown>();
        expect(e.detail).toEqual({ field: "name" });
      } else {
        throw e;
      }
    }
  });
});

describe("send: raw request body", () => {
  const spec: EndpointSpec = {
    method: "PUT",
    path: "/files/{id}",
    pathParams: { id: "id" },
    queryParams: {},
    headerParams: { content_type: "Content-Type" },
    body: { kind: "raw" },
    accept: "application/json",
    security: [],
  };

  async function sendRaw(wire: unknown, body: BodyInit | undefined): Promise<Request> {
    let seen: Request | undefined;
    const client = createClient({
      baseURL: "http://api.test",
      fetch: (async (input: RequestInfo | URL, init?: RequestInit) => {
        seen = new Request(input, init);
        return new Response(null, { status: 204 });
      }) as typeof fetch,
    });
    unwrap(await send(client, spec, wire, undefined, body));
    return seen!;
  }

  it("passes the body through and sets no content type of its own", async () => {
    const req = await sendRaw({ id: "a" }, new Uint8Array([1, 2, 3]).buffer);
    expect(req.url).toBe("http://api.test/files/a");
    expect(req.headers.get("content-type")).toBeNull();
    expect(new Uint8Array(await req.arrayBuffer())).toEqual(new Uint8Array([1, 2, 3]));
  });

  it("keeps the content type of a Blob, unless a header parameter sets one", async () => {
    const blob = new Blob(["hello"], { type: "text/plain" });
    expect((await sendRaw({ id: "a" }, blob)).headers.get("content-type")).toBe("text/plain");
    const req = await sendRaw({ id: "a", content_type: "application/zip" }, blob);
    expect(req.headers.get("content-type")).toBe("application/zip");
    expect(await req.text()).toBe("hello");
  });

  it("sends a stream with half duplex", async () => {
    const stream = new Blob(["streamed"]).stream();
    expect(await (await sendRaw({ id: "a" }, stream)).text()).toBe("streamed");
  });

  it("sends no body when none is given", async () => {
    expect((await sendRaw({ id: "a" }, undefined)).body).toBeNull();
  });
});
