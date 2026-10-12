import { describe, expect, expectTypeOf, it } from "vitest";
import * as z from "zod";

import {
  type ClientError,
  errorMessage,
  GoaError,
  goaErrorDecoder,
  isGoaError,
  NetworkError,
  type OtherErrorName,
  RequestValidationError,
  ResponseValidationError,
  UnexpectedResponseError,
} from "../src/index.js";

const goaBody = (name: string, extra: Record<string, unknown> = {}): string =>
  JSON.stringify({ name, id: "abc123", message: `${name} happened`, temporary: false, timeout: false, fault: false, ...extra });

function response(status: number, body: string, headers: Record<string, string> = {}): Response {
  return new Response(body, { status, headers });
}

const table = {
  404: [{ name: "not_found" }],
  400: [{ name: "precondition_failed" }, { name: "bad_request" }],
  422: [{ name: "invalid", schema: z.object({ field: z.string(), message: z.string() }) }],
  502: [{ name: "bad_upstream" }],
} as const;

describe("goaErrorDecoder", () => {
  const decode = goaErrorDecoder(table);

  it("uses the goa-error header and the body", async () => {
    const res = response(404, goaBody("not_found"), {
      "content-type": "application/vnd.goa.error",
      "goa-error": "not_found",
    });
    const e = await decode(res);
    expect(e).toBeInstanceOf(GoaError);
    if (!isGoaError(e)) throw e;
    expect(e.kind).toBe("goa");
    expect(e.name).toBe("not_found");
    expect(e.message).toBe("not_found happened");
    expect(e.id).toBe("abc123");
    expect(e.status).toBe(404);
    expect(e.headers.get("goa-error")).toBe("not_found");
    expect(e.body).toMatchObject({ name: "not_found" });
    expect(e.response).toBe(res);
    expect(e.detail).toBeUndefined();
  });

  it("falls back to the body name when the header is missing", async () => {
    const e = await decode(response(400, goaBody("bad_request"), { "content-type": "application/json" }));
    if (!isGoaError(e)) throw e;
    expect(e.name).toBe("bad_request");
  });

  it("prefers the header over the body name", async () => {
    const e = await decode(
      response(400, goaBody("something_else"), {
        "content-type": "application/vnd.goa.error",
        "goa-error": "precondition_failed",
      }),
    );
    if (!isGoaError(e)) throw e;
    expect(e.name).toBe("precondition_failed");
  });

  it("carries the temporary, timeout and fault flags", async () => {
    const e = await decode(
      response(502, goaBody("bad_upstream", { fault: true, temporary: true }), {
        "content-type": "application/vnd.goa.error",
      }),
    );
    if (!isGoaError(e)) throw e;
    expect([e.temporary, e.timeout, e.fault]).toEqual([true, false, true]);
  });

  it("returns a GoaError for an undeclared status with a goa body", async () => {
    const e = await decode(
      response(400, goaBody("missing_field"), { "content-type": "application/vnd.goa.error" }),
    );
    if (!isGoaError(e)) throw e;
    expect(e.name).toBe("missing_field");
    const undeclared = await goaErrorDecoder({})(
      response(500, goaBody("fault"), { "content-type": "application/vnd.goa.error" }),
    );
    if (!isGoaError(undeclared)) throw undeclared;
    expect(undeclared.name).toBe("fault");
    expect(undeclared.status).toBe(500);
  });

  it("returns UnexpectedResponseError for a body that is not a goa error", async () => {
    const res = response(502, "<html>Bad Gateway</html>", { "content-type": "text/html" });
    const e = await decode(res);
    expect(e).toBeInstanceOf(UnexpectedResponseError);
    if (!(e instanceof UnexpectedResponseError)) throw e;
    expect(e.kind).toBe("unexpected_response");
    expect(e.status).toBe(502);
    expect(e.body).toBe("<html>Bad Gateway</html>");
    expect(e.goa).toBeUndefined();
    expect(e.message).toBe("unexpected response: HTTP 502");
  });

  it("returns UnexpectedResponseError for JSON that is not a goa error", async () => {
    const e = await decode(response(401, '{"error":"nope"}', { "content-type": "application/json" }));
    expect(e).toBeInstanceOf(UnexpectedResponseError);
  });

  it("returns UnexpectedResponseError when a declared status carries a foreign body", async () => {
    // A proxy answering 404 is not the service's not_found.
    const e = await decode(response(404, '{"error":"no route"}', { "content-type": "application/json" }));
    expect(e).toBeInstanceOf(UnexpectedResponseError);
    const html = await decode(response(404, "<html>Not Found</html>", { "content-type": "text/html" }));
    expect(html).toBeInstanceOf(UnexpectedResponseError);
  });

  it("trusts the goa-error header when the body is not goa's default error", async () => {
    const e = await decode(
      response(404, '{"code":7,"message":"gone fishing"}', {
        "content-type": "application/json",
        "goa-error": "not_found",
      }),
    );
    if (!isGoaError(e)) throw e;
    expect(e.name).toBe("not_found");
    expect(e.message).toBe("gone fishing");
    expect(e.body).toEqual({ code: 7, message: "gone fishing" });
    const text = await decode(response(404, "nope", { "content-type": "text/plain", "goa-error": "not_found" }));
    if (!isGoaError(text)) throw text;
    expect(text.message).toBe("not_found: HTTP 404");
    expect(text.body).toBe("nope");
  });

  it("decodes a custom error type into detail", async () => {
    const e = await decode(
      response(422, '{"field":"name","message":"too short"}', {
        "content-type": "application/json",
        "goa-error": "invalid",
      }),
    );
    if (!isGoaError(e)) throw e;
    expect(e.name).toBe("invalid");
    expect(e.detail).toEqual({ field: "name", message: "too short" });
    expect(e.message).toBe("too short");
  });

  it("uses the table when a status declares a single error and nothing names it", async () => {
    const e = await decode(response(422, '{"field":"name","message":"too short"}', { "content-type": "application/json" }));
    if (!isGoaError(e)) throw e;
    expect(e.name).toBe("invalid");
  });

  it("returns ResponseValidationError when a custom error body is malformed", async () => {
    const e = await decode(
      response(422, '{"field":1}', { "content-type": "application/json", "goa-error": "invalid" }),
    );
    expect(e).toBeInstanceOf(ResponseValidationError);
  });

  it("narrows declared names and details", async () => {
    const e = await decode(response(404, goaBody("not_found"), { "content-type": "application/vnd.goa.error" }));
    expectTypeOf(e).toEqualTypeOf<
      | GoaError<"not_found">
      | GoaError<"precondition_failed">
      | GoaError<"bad_request">
      | GoaError<"invalid", { field: string; message: string }>
      | GoaError<"bad_upstream">
      | GoaError<OtherErrorName>
      | UnexpectedResponseError
      | ResponseValidationError
    >();
    if (e.kind === "goa" && e.name === "invalid") {
      expectTypeOf(e.detail).toEqualTypeOf<{ field: string; message: string } | undefined>();
    }
    type Empty = Awaited<ReturnType<ReturnType<typeof goaErrorDecoder<{}>>>>;
    expectTypeOf<Empty>().toEqualTypeOf<
      GoaError<OtherErrorName> | UnexpectedResponseError | ResponseValidationError
    >();
  });
});

describe("UnexpectedResponseError", () => {
  it("exposes a goa error body it was handed", () => {
    const e = new UnexpectedResponseError(response(200, goaBody("odd")), goaBody("odd"));
    expect(e.goa).toMatchObject({ name: "odd", message: "odd happened" });
    expect(e.message).toBe("odd happened");
  });
});

describe("errorMessage", () => {
  it("renders every kind of failure", () => {
    const issues = z.safeParse(z.object({ a: z.string() }), { a: 1 }).error!.issues;
    expect(errorMessage(new NetworkError("offline"))).toBe("offline");
    expect(errorMessage(new RequestValidationError(issues))).toContain("invalid request");
    expect(errorMessage(new RequestValidationError(issues))).toContain("a");
    expect(errorMessage(new ResponseValidationError(issues))).toContain("invalid response");
    expect(errorMessage("plain")).toBe("plain");
    expect(errorMessage({ code: 1 })).toBe('{"code":1}');
    expect(errorMessage(undefined)).toBe("undefined");
  });

  it("discriminates the client error union on kind", () => {
    const describeError = (e: ClientError | GoaError): string => {
      switch (e.kind) {
        case "goa":
          return e.name;
        case "network":
        case "abort":
        case "timeout":
          return "transport";
        case "request_validation":
        case "response_validation":
          return e.pretty();
        case "unexpected_response":
          return String(e.status);
      }
    };
    expect(describeError(new NetworkError("x"))).toBe("transport");
  });
});
