/** Next sends the request on to the next middleware, or to fetch. */
export type Next = (req: Request) => Promise<Response>;

/**
 * Middleware wraps every request the client sends. Call `next(req)` to
 * continue, or return a Response to short-circuit.
 *
 * A middleware that reads a response body must read `response.clone()`:
 * reading the original consumes it, which breaks decoding and ends streams.
 */
export type Middleware = (req: Request, next: Next) => Promise<Response>;

/** compose chains middleware onion style: the first entry is outermost. */
export function compose(middleware: readonly Middleware[], terminal: Next): Next {
  let next = terminal;
  for (let i = middleware.length - 1; i >= 0; i--) {
    const mw = middleware[i]!;
    const inner = next;
    next = (req) => mw(req, inner);
  }
  return next;
}
