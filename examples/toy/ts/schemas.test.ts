import {
  decode,
  decodeJSON,
  encode,
  RequestValidationError,
  Thing,
  ThingEvent,
  ThingEventEnvelope,
  ThingFilter,
  ThingInput,
  ThingKind,
  toy,
  unwrap,
} from "toy-client";
import { describe, expect, expectTypeOf, it } from "vitest";
import * as z from "zod";

import { client } from "./helpers.js";

const id = "11111111-1111-4111-8111-111111111111";

describe("request validation", () => {
  it("rejects an invalid request without sending it", async () => {
    let sent = 0;
    const counting = client({
      middleware: [
        (req, next) => {
          sent++;
          return next(req);
        },
      ],
    });
    const result = await toy.streamPostSse(counting, {
      // @ts-expect-error "sprocket" is not a kind the design allows
      filter: { kinds: ["sprocket"] },
    });
    expect(result.ok).toBe(false);
    const error = result.error!;
    expect(error).toBeInstanceOf(RequestValidationError);
    if (!(error instanceof RequestValidationError)) throw error;
    expect(error.kind).toBe("request_validation");
    expect(error.issues[0]?.path).toEqual(["filter", "kinds", 0]);
    expect(error.pretty()).toContain("filter.kinds[0]");
    expect(sent).toBe(0);
  });

  it("rejects values of the wrong type", async () => {
    const result = await toy.streamThingsSse(client(), { id: "abc", delay_ms: 1.5 });
    expect(result.error).toBeInstanceOf(RequestValidationError);
    const invalidDate = await toy.streamWrappedSse(client(), { id: "abc", since: new Date("nope") });
    expect(invalidDate.error).toBeInstanceOf(RequestValidationError);
  });

  it("makes defaulted request attributes optional", () => {
    expectTypeOf<toy.StreamWrappedSsePayload["count"]>().toEqualTypeOf<number | undefined>();
    expectTypeOf<toy.StreamViewedSsePayload>().toEqualTypeOf<{ view?: "default" | "extended" | undefined }>();
  });
});

describe("types and schemas share a name", () => {
  it("infers the declared type from the schema", () => {
    expectTypeOf<z.output<typeof ThingFilter>>().toEqualTypeOf<ThingFilter>();
    expectTypeOf<z.output<typeof ThingEvent>>().toEqualTypeOf<ThingEvent>();
    expectTypeOf<z.output<typeof ThingEventEnvelope>>().toEqualTypeOf<ThingEventEnvelope>();
    // A recursive schema is annotated with its type.
    expectTypeOf<z.output<typeof Thing>>().toEqualTypeOf<Thing>();
    expectTypeOf<Thing["parent"]>().toEqualTypeOf<Thing | undefined>();
    expectTypeOf<Thing["kind"]>().toEqualTypeOf<"widget" | "gadget">();
    expectTypeOf<Thing["created_at"]>().toEqualTypeOf<Date | undefined>();
    expectTypeOf<Thing["born_on"]>().toEqualTypeOf<string | undefined>();
    expectTypeOf<Thing["blob"]>().toEqualTypeOf<Uint8Array | undefined>();
    expectTypeOf<Thing["legacyName"]>().toEqualTypeOf<string | undefined>();
  });
});

describe("ThingKind: an inline enum named with Meta(\"typescript:name\")", () => {
  it("is a type and a schema of its own, used by the parent", () => {
    expectTypeOf<ThingKind>().toEqualTypeOf<"widget" | "gadget">();
    expectTypeOf<Thing["kind"]>().toEqualTypeOf<ThingKind>();
    expectTypeOf<z.output<typeof ThingKind>>().toEqualTypeOf<ThingKind>();
    expect(unwrap(decode(ThingKind, "gadget"))).toBe("gadget");
    expect(decode(ThingKind, "sprocket").ok).toBe(false);
    // The default belongs to the parent, not to the enum.
    expect(decode(ThingKind, undefined).ok).toBe(false);
    expect(unwrap(decode(Thing, { id })).kind).toBe("widget");
  });
});

describe("ThingInput: the input side of a type with defaults", () => {
  it("makes the defaulted attributes optional and nothing else", () => {
    expectTypeOf<ThingInput["kind"]>().toEqualTypeOf<ThingKind | undefined>();
    expectTypeOf<ThingInput["enabled"]>().toEqualTypeOf<boolean | undefined>();
    expectTypeOf<ThingInput["id"]>().toEqualTypeOf<string>();
    // Nested references use the input side too.
    expectTypeOf<ThingInput["parent"]>().toEqualTypeOf<ThingInput | undefined>();
    expectTypeOf<ThingEvent["thing"]>().toEqualTypeOf<Thing | undefined>();
    expectTypeOf<z.output<typeof ThingInput>>().toEqualTypeOf<ThingInput>();
  });

  it("encodes a hand-built value without inventing the defaults", () => {
    const fixture: ThingInput = { id, name: "fixture", parent: { id } };
    expect(unwrap(encode(ThingInput, fixture))).toEqual({ id, name: "fixture", parent: { id } });
    expect(unwrap(decode(ThingInput, { id }))).toEqual({ id });
    // A decoded Thing is a ThingInput; the reverse needs the defaults.
    const thing: Thing = unwrap(decode(Thing, { id }));
    const asInput: ThingInput = thing;
    expect(asInput.kind).toBe("widget");
    // @ts-expect-error kind and enabled are required once decoded
    const back: Thing = fixture;
    expect(back).toBe(fixture);
  });
});

describe("Thing", () => {
  it("fills defaults when decoding", () => {
    expect(unwrap(decode(Thing, { id }))).toEqual({ id, kind: "widget", enabled: true });
  });

  it("round-trips through its wire form", () => {
    const wire = {
      id,
      name: "thing-1",
      kind: "gadget",
      enabled: false,
      created_at: "2026-01-02T03:04:05.000Z",
      born_on: "2026-01-02",
      count: 3,
      blob: "aGk=",
      payload: { free: ["form"] },
      tags: ["a"],
      attrs: { x: 1 },
      labels: [{ key: "k", value: "v" }],
      parent: { id, kind: "widget", enabled: true, shape: { type: "square", value: { side: 1 } } },
      shape: { type: "circle", value: { radius: 2 } },
      legacyName: "old",
    };
    const local = unwrap(decode(Thing, wire));
    expect(local.created_at).toEqual(new Date("2026-01-02T03:04:05Z"));
    expect(local.blob).toEqual(new TextEncoder().encode("hi"));
    expect(unwrap(encode(Thing, local))).toEqual(wire);
  });

  it("enforces the design's validations in both directions", () => {
    const invalid: [string, Record<string, unknown>][] = [
      ["id is not a UUID", { id: "nope" }],
      ["name is too long", { id, name: "x".repeat(33) }],
      ["name does not match the pattern", { id, name: "Upper" }],
      ["kind is not in the enumeration", { id, kind: "sprocket" }],
      ["count is negative", { id, count: -1 }],
      ["count is past the safe integer range", { id, count: 2 ** 53 }],
      ["small overflows int32", { id, small: 2 ** 31 }],
      ["tags is empty", { id, tags: [] }],
      ["tags has too many elements", { id, tags: ["1", "2", "3", "4", "5"] }],
      ["born_on is not a date", { id, born_on: "2026-01-02T00:00:00Z" }],
      ["shape names an unknown branch", { id, shape: { type: "hexagon", value: {} } }],
      ["a nested thing is invalid", { id, parent: { id: "nope" } }],
    ];
    for (const [name, wire] of invalid) {
      expect(decode(Thing, wire).ok, `decode: ${name}`).toBe(false);
    }
    const valid = unwrap(decode(Thing, { id }));
    expect(encode(Thing, { ...valid, name: "Upper" }).ok).toBe(false);
    expect(encode(Thing, { ...valid, tags: [] }).ok).toBe(false);
    expect(encode(Thing, { ...valid, created_at: new Date("nope") }).ok).toBe(false);
  });

  it("ignores attributes it does not know, so servers can add some", () => {
    expect(unwrap(decode(Thing, { id, added_later: true }))).toEqual({ id, kind: "widget", enabled: true });
  });

  it("parses JSON text, as a WebSocket or storage consumer would", () => {
    const event = unwrap(decodeJSON(ThingEvent, JSON.stringify({ id: "e1", thing: { id } })));
    expect(event).toEqual({ type: "thing", id: "e1", thing: { id, kind: "widget", enabled: true } });
    expect(decodeJSON(ThingEvent, "{").ok).toBe(false);
  });
});
