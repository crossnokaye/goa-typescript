# ADR 0004: Input-side types for declared types with defaults

- **Date:** 2026-10-12
- **Status:** Accepted
- **Supersedes:** none; refines ADR 0001, "One Zod 4 codec per type" and "Hoist only what the design names"

## Context

A declared type gets one TypeScript type, and it is the output side: the shape of a decoded value.

```go
Attribute("pending", Boolean, func() { Default(false) })
```

```ts
export type Snapshot = { pending: boolean /* ... */ };
export const Snapshot = z.object({ pending: z.boolean().default(false) /* ... */ });
```

That is right for a decoded value: decoding fills the default in, so the attribute is always there. It is wrong for a value somebody builds. Two cases came up (issue #4):

- **Fixtures and mocks.** Everything built by hand has to spell every defaulted attribute out. A client migrating from an SDK that typed those attributes as optional touches every fixture.
- **Nested request payloads.** At the top level of a request a defaulted attribute was already optional, and omitted, because the server applies the default. One level down it was not: the request referred to `Snapshot`, so a caller had to send the values the server would have defaulted. The issue flagged this as unverified; the toy's `echo_thing` method now shows it.

Zod has an answer that looks ready-made: every schema has an input type and an output type, `z.input<typeof Snapshot>` and `z.output<typeof Snapshot>`, and `.default()` is exactly what makes them differ. The README could document `z.input` as the type for hand-built values.

It cannot. Every generated schema is a codec, wire to local: `dateTime()` turns an RFC 3339 string into a `Date`, `bytes()` turns base64 into a `Uint8Array`. So `z.input<typeof Snapshot>` is the **wire** shape (string dates, base64 bytes), and `encode(schema, value)` takes `z.output<T>`, the local shape. A hand-built value typed as `z.input` would not be encodable, and the decoded shape with optional defaults is not a type Zod has a name for.

## Decision

**Every declared type whose input side differs gets a second declaration beside it, `NameInput`, and request payloads refer to it.**

- `NameInput` is the same object type and schema with every attribute that has a `Default` optional (`.optional()` in place of `.default(...)`, and the "Default: x (applied by the server when omitted)" note the top level of a request already carries). Where `Name` refers to a declared type that has a variant, `NameInput` refers to `ThatInput`; arrays and maps follow (`ThatInput[]`). A recursive type refers to its own variant through the same lazy schema the base type uses.
- "Differs" means: the type has a defaulted attribute, in its own object or in an inline object inside it, or it refers, directly or through other declared types, to a type that differs. Nothing is emitted otherwise. A named array or map of a differing type differs too.
- The name is `Name` + `Input`, run through the same scope as every other declaration: a design type that is already called `SnapshotInput` keeps its name and the variant becomes `SnapshotInput2`.
- A request payload (`<Method>Payload`) is rendered on the input side: a nested declared type is referred to by its variant, and a defaulted attribute of an inline object is optional, at any depth. The top-level rule is unchanged.
- Results, events and errors keep the base type: a decoded value has every defaulted attribute, and that is still the only type a response can produce.
- `NameInput` is the documented type for a value built by hand. A decoded `Name` is assignable to `NameInput`, so a value from a response can be sent back as it is.

## Consequences

### Positive

- A nested defaulted attribute no longer has to be sent; the server fills it in, as the design says.
- Fixtures and mocks have a type that matches how they are written. A client migrating from optional-defaults types changes the type name on its fixtures, not the fixtures.
- Call sites that read decoded values see no change: `Thing` means what it meant.

### Negative / breaking

- `types.ts` grows by one declaration per differing type, with the base type's documentation repeated in shortened form. A design with many defaults doubles its types file.
- A request's nested types change name: `filter: Filter` becomes `filter: FilterInput`. Code that annotated such a value as `Filter` still compiles (a `Filter` is a `FilterInput`); code that relied on `z.input<typeof FilterInput>` or named the payload's nested type does not. Nothing had been published.
- Two names for one design type is something to learn. The generated JSDoc on `NameInput` says what it is for.

### Neutral

- Whether a type differs is decided while building the spec (`typeMapper.markInputs`), not in the templates, which only render the second declaration when it is there.
- A variant is emitted whether or not a request uses it: the type is useful on its own, as the base types are.

## Alternatives considered

1. **Document `z.input<typeof Name>` as the type for hand-built values.** Rejected: it is the wire shape, because the schemas are codecs (above). It would have been right only for types with no date, bytes or nested codec, which is not a distinction a reader can be asked to keep.
2. **Make defaulted attributes optional in the one type, and have decoding fill them in anyway.** Rejected. The decoded value would be typed looser than it is, and every read of a defaulted attribute would need a check or a `!` for a value that is always present.
3. **A generic `Input<T>` mapped type in the runtime.** TypeScript cannot tell a defaulted attribute from a required one once the type is declared, so the mapped type would need a per-type list of defaulted keys, which is the same information as a second declaration with none of the schema. The schema is needed: `encode(Name, fixture)` would reject a fixture without its defaults.
4. **Emit the variant for every declared type, differing or not.** Rejected: twice the declarations for nothing, and `NameInput` would then mean "maybe the same as `Name`". Emitting it only when it differs makes its presence informative.
5. **A migration note only.** Documents the fixture cost without removing it, and leaves nested request payloads asking for values the server would default.

## References

- GitHub issue #4.
- `plugin/typescript/internal/tsgen/types.go` (`typeMapper.markInputs`, `renderCtx.input`), `build_request.go`, `templates/types.ts.tpl`.
- `examples/toy/design/toy.go` (`echo_thing`), `examples/toy/ts/rest.test.ts`, `examples/toy/ts/schemas.test.ts`.
- ADR 0001, "One Zod 4 codec per type" and "Hoist only what the design names".
