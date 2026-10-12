/**
 * Type-level checks of unwrap and unwrapAsync as a consumer sees them.
 *
 * This file is compiled by tsconfig.consumer.json WITHOUT
 * `exactOptionalPropertyTypes`, the default for an application that uses a
 * generated client. Under that setting the failure arm's `value?: never` also
 * admits `undefined`, which is enough for structural inference against a
 * `CallResult` to pick up both arms and produce `T | undefined`. Nothing here
 * runs: expectTypeOf is a no-op at runtime and tsc enforces the assertions.
 */
import { expectTypeOf } from "vitest";

import { type CallResult, type Result, unwrap, unwrapAsync } from "../../src/index.js";

interface ThingReply {
  readonly thing: { readonly name: string };
}

declare const call: CallResult<ThingReply, Error>;
declare const pending: Promise<CallResult<ThingReply, Error>>;
declare const plain: Result<ThingReply, Error>;
declare const pendingPlain: Promise<Result<ThingReply, Error>>;
declare const nothing: CallResult<void, Error>;

// A CallResult, as a generated function returns it.
expectTypeOf(unwrap(call)).toEqualTypeOf<ThingReply>();
expectTypeOf(unwrapAsync(pending)).toEqualTypeOf<Promise<ThingReply>>();

// A CallResult passed through a generic wrapper, as a query helper would.
function via<T, E>(p: Promise<CallResult<T, E>>): Promise<T> {
  return unwrapAsync(p);
}
expectTypeOf(via(pending)).toEqualTypeOf<Promise<ThingReply>>();

// A method with no result.
expectTypeOf(unwrap(nothing)).toEqualTypeOf<void>();
expectTypeOf(unwrapAsync(Promise.resolve(nothing))).toEqualTypeOf<Promise<void>>();

// A plain Result is unaffected.
expectTypeOf(unwrap(plain)).toEqualTypeOf<ThingReply>();
expectTypeOf(unwrapAsync(pendingPlain)).toEqualTypeOf<Promise<ThingReply>>();
