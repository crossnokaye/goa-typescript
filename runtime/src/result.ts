/**
 * Result is the return shape of every generated function: a discriminated
 * union on `ok`. Nothing in the client throws except `unwrap`/`unwrapAsync`.
 */
export type Result<T, E = unknown> =
  | { ok: true; value: T; error?: never }
  | { ok: false; value?: never; error: E };

/** ok wraps a success value. */
export function ok<T>(value: T): Result<T, never> {
  return { ok: true, value };
}

/** err wraps a failure value. */
export function err<E>(error: E): Result<never, E> {
  return { ok: false, error };
}

/** unwrap returns the success value or throws the error. */
export function unwrap<T, E>(result: Result<T, E>): T {
  if (!result.ok) {
    throw result.error;
  }
  return result.value;
}

/** unwrapAsync awaits a Result and returns the success value or throws the error. */
export async function unwrapAsync<T, E>(result: Promise<Result<T, E>>): Promise<T> {
  return unwrap(await result);
}
