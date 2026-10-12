import { type Client, type ClientOptions, createClient } from "toy-client";

function env(name: string): string {
  const value = process.env[name];
  if (!value) {
    throw new Error(
      `${name} is not set: start the toy server (go run ./cmd/toy) and pass its address, ` +
        "or run the pipeline with `go test ./plugin/typescript` from the repository root",
    );
  }
  return value;
}

/** baseURL is the address the toy server printed on startup. */
export const baseURL = env("TOY_BASE_URL");

/** token is the credential the toy server accepts for both of its schemes. */
export const token = env("TOY_TOKEN");

/** client returns a client for the toy server, authenticated unless told otherwise. */
export function client(options: Partial<ClientOptions> = {}): Client {
  return createClient({ baseURL, auth: token, ...options });
}

/** collect drains an async iterable into an array. */
export async function collect<T>(source: AsyncIterable<T>): Promise<T[]> {
  const out: T[] = [];
  for await (const item of source) {
    out.push(item);
  }
  return out;
}
