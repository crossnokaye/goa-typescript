/**
 * Result is the return shape of every generated function: a discriminated
 * union on `ok`. Nothing in the client throws except `unwrap`/`unwrapAsync`.
 */
export type Result<T, E = unknown> =
  | { ok: true; value: T; error?: never }
  | { ok: false; value?: never; error: E };

/**
 * CallResult is what a generated function returns: a `Result` with the fetch
 * `Response` beside the value, for the status and the headers.
 *
 * On success `response` is always set. On failure it is set whenever a response
 * arrived (a goa error, an unexpected response, a response that fails
 * validation) and absent otherwise (network failure, abort, timeout, a request
 * that fails validation).
 *
 * A CallResult is a Result, so `unwrap` and `unwrapAsync` take it as is.
 */
export type CallResult<T, E = unknown> =
  | { ok: true; value: T; error?: never; response: Response }
  | { ok: false; value?: never; error: E; response?: Response | undefined };

/** ok wraps a success value. */
export function ok<T>(value: T): Result<T, never> {
  return { ok: true, value };
}

/** err wraps a failure value. */
export function err<E>(error: E): Result<never, E> {
  return { ok: false, error };
}

/**
 * OkValue is the success value type of any Result-shaped type.
 *
 * `unwrap` reads its value type from the success arm alone rather than from
 * `Result<T, E>`. Inferring `T` structurally from a `CallResult` would also
 * visit the failure arm, whose `value?: never` admits `undefined` when the
 * caller compiles without `exactOptionalPropertyTypes`, and yield `T | undefined`.
 */
type OkValue<R> = R extends { ok: true; value: infer T } ? T : never;

/** unwrap returns the success value or throws the error. */
export function unwrap<R extends Result<unknown, unknown>>(result: R): OkValue<R> {
  if (!result.ok) {
    throw result.error;
  }
  return result.value as OkValue<R>;
}

/** unwrapAsync awaits a Result and returns the success value or throws the error. */
export async function unwrapAsync<R extends Result<unknown, unknown>>(result: Promise<R>): Promise<OkValue<R>> {
  return unwrap(await result);
}
