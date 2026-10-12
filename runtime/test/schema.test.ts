import { describe, expect, expectTypeOf, it } from "vitest";
import * as z from "zod";

import {
  any,
  bytes,
  date,
  dateTime,
  decode,
  decodeJSON,
  encode,
  encodeJSON,
  ErrorResultSchema,
  float,
  int,
  int64,
  nullAsAbsent,
  RequestValidationError,
  ResponseValidationError,
  uint,
  unwrap,
} from "../src/index.js";

describe("dateTime", () => {
  it("decodes RFC 3339 with offsets and long fractions", () => {
    expect(unwrap(decode(dateTime(), "2026-01-02T03:04:05Z"))).toEqual(new Date("2026-01-02T03:04:05Z"));
    expect(unwrap(decode(dateTime(), "2026-01-02T03:04:05-07:00"))).toEqual(new Date("2026-01-02T10:04:05Z"));
    expect(unwrap(decode(dateTime(), "2026-01-02T03:04:05.123456789Z"))).toEqual(
      new Date("2026-01-02T03:04:05.123Z"),
    );
  });

  it("encodes to ISO 8601 UTC", () => {
    expect(unwrap(encode(dateTime(), new Date("2026-01-02T03:04:05.5Z")))).toBe("2026-01-02T03:04:05.500Z");
  });

  it("rejects non-dates both ways", () => {
    expect(decode(dateTime(), "2026-01-02").ok).toBe(false);
    expect(decode(dateTime(), 12).ok).toBe(false);
    expect(encode(dateTime(), new Date("nope")).ok).toBe(false);
    expect(encode(dateTime(), "2026-01-02T03:04:05Z" as unknown as Date).ok).toBe(false);
  });
});

describe("date", () => {
  it("stays a string", () => {
    const value = unwrap(decode(date(), "2026-01-02"));
    expect(value).toBe("2026-01-02");
    expectTypeOf(value).toEqualTypeOf<string>();
    expect(unwrap(encode(date(), "2026-01-02"))).toBe("2026-01-02");
    expect(decode(date(), "2026-13-02").ok).toBe(false);
    expect(decode(date(), "2026-01-02T00:00:00Z").ok).toBe(false);
  });
});

describe("bytes", () => {
  it("round-trips standard padded base64", () => {
    for (const text of ["", "f", "fo", "foo", "foob", "fooba", "foobar"]) {
      const raw = new TextEncoder().encode(text);
      const wire = unwrap(encode(bytes(), raw));
      expect(wire).toBe(Buffer.from(text).toString("base64"));
      expect(unwrap(decode(bytes(), wire))).toEqual(raw);
    }
  });

  it("round-trips every byte value and large inputs", () => {
    const raw = new Uint8Array(100_000).map((_, i) => i % 256);
    expect(unwrap(decode(bytes(), unwrap(encode(bytes(), raw))))).toEqual(raw);
  });

  it("rejects unpadded or URL-safe base64", () => {
    expect(decode(bytes(), "Zm9").ok).toBe(false);
    expect(decode(bytes(), "a-_b").ok).toBe(false);
    expect(encode(bytes(), "Zm9v" as unknown as Uint8Array).ok).toBe(false);
  });
});

describe("numbers", () => {
  it("bounds integers to the safe range", () => {
    expect(decode(int(), 2 ** 53 - 1).ok).toBe(true);
    expect(decode(int(), 2 ** 53).ok).toBe(false);
    expect(decode(int64(), -(2 ** 53)).ok).toBe(false);
    expect(decode(int(), 1.5).ok).toBe(false);
    expect(decode(uint(), -1).ok).toBe(false);
    expect(decode(uint(), 0).ok).toBe(true);
    expect(decode(float(), 1.5).ok).toBe(true);
    expect(decode(float(), "1.5").ok).toBe(false);
    expect(unwrap(decode(any(), { a: [1] }))).toEqual({ a: [1] });
  });
});

describe("objects", () => {
  const Circle = z.object({ radius: z.number() });
  const Thing = z.object({
    id: z.uuid(),
    kind: z.enum(["a", "b"]).default("a"),
    created_at: dateTime().optional(),
    blob: bytes().optional(),
    tags: z.array(z.string()).max(2).optional(),
    shape: z
      .discriminatedUnion("type", [
        z.object({ type: z.literal("circle"), value: Circle }),
        z.object({ type: z.literal("at"), value: dateTime() }),
      ])
      .optional(),
  });
  const id = "550e8400-e29b-41d4-a716-446655440000";

  it("fills defaults on decode only", () => {
    expect(unwrap(decode(Thing, { id }))).toEqual({ id, kind: "a" });
    expect(unwrap(encode(Thing, { id, kind: "b" }))).toEqual({ id, kind: "b" });
  });

  it("applies optional codecs both ways", () => {
    const local = unwrap(decode(Thing, { id, created_at: "2026-01-02T03:04:05Z", blob: "aGk=" }));
    expect(local.created_at).toEqual(new Date("2026-01-02T03:04:05Z"));
    expect(local.blob).toEqual(new TextEncoder().encode("hi"));
    expect(unwrap(encode(Thing, local))).toEqual({
      id,
      kind: "a",
      created_at: "2026-01-02T03:04:05.000Z",
      blob: "aGk=",
    });
  });

  it("decodes and encodes union envelopes", () => {
    const circle = unwrap(decode(Thing, { id, shape: { type: "circle", value: { radius: 2 } } }));
    expect(circle.shape).toEqual({ type: "circle", value: { radius: 2 } });
    const at = unwrap(decode(Thing, { id, shape: { type: "at", value: "2026-01-02T03:04:05Z" } }));
    expect(at.shape).toEqual({ type: "at", value: new Date("2026-01-02T03:04:05Z") });
    expect(unwrap(encode(Thing, at)).shape).toEqual({ type: "at", value: "2026-01-02T03:04:05.000Z" });
    expect(decode(Thing, { id, shape: { type: "hexagon", value: {} } }).ok).toBe(false);
  });

  it("strips unknown keys both ways", () => {
    expect(unwrap(decode(Thing, { id, future: 1 }))).toEqual({ id, kind: "a" });
    expect(unwrap(encode(Thing, { id, kind: "a", future: 1 } as z.output<typeof Thing>))).toEqual({ id, kind: "a" });
  });

  it("runs validations in both directions", () => {
    const tooMany = { id, kind: "a" as const, tags: ["1", "2", "3"] };
    const decoded = decode(Thing, tooMany);
    expect(decoded.ok).toBe(false);
    expect(decoded.error).toBeInstanceOf(ResponseValidationError);
    const encoded = encode(Thing, tooMany);
    expect(encoded.ok).toBe(false);
    expect(encoded.error).toBeInstanceOf(RequestValidationError);
    expect(encoded.error?.pretty()).toContain("tags");
    expect(encoded.error?.issues[0]?.path).toEqual(["tags"]);
  });
});

describe("nullAsAbsent", () => {
  const Thing = z.object({ legacyName: nullAsAbsent(z.string().min(2)), at: nullAsAbsent(dateTime()) });

  it("treats null and missing as absent on decode", () => {
    expect(unwrap(decode(Thing, { legacyName: null, at: null }))).toEqual({});
    expect(unwrap(decode(Thing, {}))).toEqual({});
    expectTypeOf(unwrap(decode(Thing, {}))).toEqualTypeOf<{ legacyName?: string | undefined; at?: Date | undefined }>();
  });

  it("decodes, validates and encodes present values", () => {
    const local = unwrap(decode(Thing, { legacyName: "ab", at: "2026-01-02T03:04:05Z" }));
    expect(local).toEqual({ legacyName: "ab", at: new Date("2026-01-02T03:04:05Z") });
    expect(unwrap(encode(Thing, local))).toEqual({ legacyName: "ab", at: "2026-01-02T03:04:05.000Z" });
    expect(unwrap(encode(Thing, {}))).toEqual({});
    expect(decode(Thing, { legacyName: "a" }).ok).toBe(false);
    expect(encode(Thing, { legacyName: "a" }).ok).toBe(false);
  });
});

describe("JSON helpers", () => {
  it("decodes and encodes JSON text", () => {
    const schema = z.object({ at: dateTime() });
    expect(unwrap(decodeJSON(schema, '{"at":"2026-01-02T03:04:05Z"}'))).toEqual({
      at: new Date("2026-01-02T03:04:05Z"),
    });
    expect(unwrap(encodeJSON(schema, { at: new Date(0) }))).toBe('{"at":"1970-01-01T00:00:00.000Z"}');
  });

  it("reports malformed JSON as a response validation error", () => {
    const result = decodeJSON(z.object({}), "{oops");
    expect(result.ok).toBe(false);
    expect(result.error).toBeInstanceOf(ResponseValidationError);
    expect(result.error?.message).toContain("invalid JSON");
  });

  it("validates goa's default error body", () => {
    expect(unwrap(decode(ErrorResultSchema, { name: "n", message: "m" }))).toEqual({
      name: "n",
      id: "",
      message: "m",
      temporary: false,
      timeout: false,
      fault: false,
    });
  });
});
