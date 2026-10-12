import * as z from "zod";

import { isAbort } from "./client.js";
import {
  AbortError,
  asErrorResult,
  GoaError,
  NetworkError,
  type OtherErrorName,
  RequestValidationError,
  ResponseValidationError,
  type TransportError,
  UnexpectedResponseError,
} from "./errors.js";
import { type CallResult, err, ok, type Result } from "./result.js";

/** dateTime maps goa `Format(FormatDateTime)` strings (RFC 3339, any offset) to `Date`. */
export function dateTime() {
  return z.codec(z.iso.datetime({ offset: true }), z.date(), {
    decode: (text) => new Date(text),
    encode: (date) => date.toISOString(),
  });
}

/** date validates goa `Format(FormatDate)` strings (`YYYY-MM-DD`); they stay strings. */
export function date() {
  return z.iso.date();
}

/** bytes maps goa `Bytes` (standard base64 in JSON) to `Uint8Array`. */
export function bytes() {
  return z.codec(z.base64(), z.instanceof(Uint8Array), {
    decode: (text) => base64ToBytes(text),
    encode: (value) => bytesToBase64(value),
  });
}

/** int validates goa `Int`/`Int64`: a safe integer, so values past 2^53 fail loudly. */
export function int() {
  return z.int();
}

/** int64 validates goa `Int64`; see `int`. */
export function int64() {
  return z.int();
}

/** uint validates goa `UInt`/`UInt64`: a non-negative safe integer. */
export function uint() {
  return z.int().min(0);
}

/** float validates goa `Float32`/`Float64`. */
export function float() {
  return z.number();
}

/** any validates goa `Any`. */
export function any() {
  return z.unknown();
}

/**
 * nullAsAbsent wraps the schema of an optional attribute that goa serializes
 * as `null` when unset (a `struct:tag:json` override without `omitempty`).
 * Decoding treats `null` as absent; encoding omits the value.
 */
export function nullAsAbsent<T extends z.ZodType>(schema: T) {
  return z.codec(z.unknown(), schema.optional(), {
    decode: (value) => (value ?? undefined) as z.input<T> | undefined,
    encode: (value) => value,
  });
}

/** ErrorResultSchema validates goa's default error body. */
export const ErrorResultSchema = z.object({
  name: z.string(),
  id: z.string().default(""),
  message: z.string(),
  temporary: z.boolean().default(false),
  timeout: z.boolean().default(false),
  fault: z.boolean().default(false),
});

/** decode converts a wire value (parsed JSON) to its local form and validates it. */
export function decode<T extends z.ZodType>(
  schema: T,
  wire: unknown,
): Result<z.output<T>, ResponseValidationError> {
  const result = z.safeDecode(schema, wire as z.input<T>);
  if (!result.success) {
    return err(new ResponseValidationError(result.error.issues, { cause: result.error }));
  }
  return ok(result.data);
}

/** decodeJSON parses JSON text, then decodes it. */
export function decodeJSON<T extends z.ZodType>(
  schema: T,
  json: string,
): Result<z.output<T>, ResponseValidationError> {
  let wire: unknown;
  try {
    wire = JSON.parse(json);
  } catch (cause) {
    return err(invalidJSON(json, cause));
  }
  return decode(schema, wire);
}

/** encode validates a local value and converts it to its wire form (JSON-ready). */
export function encode<T extends z.ZodType>(
  schema: T,
  value: z.output<T>,
): Result<z.input<T>, RequestValidationError> {
  const result = z.safeEncode(schema, value);
  if (!result.success) {
    return err(new RequestValidationError(result.error.issues, { cause: result.error }));
  }
  return ok(result.data);
}

/** encodeJSON encodes a local value and serializes it. */
export function encodeJSON<T extends z.ZodType>(
  schema: T,
  value: z.output<T>,
): Result<string, RequestValidationError> {
  const wire = encode(schema, value);
  if (!wire.ok) {
    return wire;
  }
  return ok(JSON.stringify(wire.value));
}

/** invalidJSON builds the validation error for a body that is not JSON. */
export function invalidJSON(input: string, cause: unknown): ResponseValidationError {
  const reason = cause instanceof Error ? cause.message : String(cause);
  return new ResponseValidationError(
    [{ code: "custom", path: [], message: `invalid JSON: ${reason}`, input }],
    { cause },
  );
}

/**
 * HeaderShape is the kind of value a response header carries. goa writes
 * numbers and booleans as text and joins the elements of an array with ", ".
 */
export type HeaderShape = "string" | "number" | "boolean" | "string[]" | "number[]" | "boolean[]";

/** ResponseHeader maps a response header onto an attribute. */
export interface ResponseHeader {
  /** name is the HTTP header name. */
  readonly name: string;
  readonly shape: HeaderShape;
}

/** ResponseHeaders maps attributes (by JSON key) to the headers that carry them. */
export type ResponseHeaders = Readonly<Record<string, ResponseHeader>>;

/** parseHeader converts a header value to the shape the design gives it. */
export function parseHeader(value: string, shape: HeaderShape): unknown {
  switch (shape) {
    case "string":
      return value;
    case "number": {
      // An unparseable number stays a string so that the schema reports it.
      const n = value.trim() === "" ? Number.NaN : Number(value);
      return Number.isNaN(n) ? value : n;
    }
    case "boolean": {
      const v = value.trim().toLowerCase();
      return v === "true" ? true : v === "false" ? false : value;
    }
    default: {
      const elem = shape.slice(0, -2) as HeaderShape;
      if (value.trim() === "") {
        return [];
      }
      // An element that contains a comma does not survive; goa has the same limit.
      return value.split(",").map((item) => parseHeader(item.trim(), elem));
    }
  }
}

/** readHeaders sets every mapped header that is present on target. */
function readHeaders(target: Record<string, unknown>, headers: Headers, mapping: ResponseHeaders): void {
  for (const [key, header] of Object.entries(mapping)) {
    const value = headers.get(header.name);
    if (value !== null) {
      target[key] = parseHeader(value, header.shape);
    }
  }
}

/** ErrorTableEntry is one error an endpoint declares for a status code. */
export interface ErrorTableEntry {
  readonly name: string;
  /** schema decodes the body of an error declared with a custom type into `detail`. */
  readonly schema?: z.ZodType;
  /** headers are the attributes of a custom error type that travel in response headers. */
  readonly headers?: ResponseHeaders;
}

/** ErrorTable lists an endpoint's declared errors by HTTP status code. */
export type ErrorTable = Readonly<Record<number, readonly ErrorTableEntry[]>>;

type EntryError<E> = E extends { readonly name: infer N extends string; readonly schema: infer S extends z.ZodType }
  ? GoaError<N, z.output<S>>
  : E extends { readonly name: infer N extends string }
    ? GoaError<N>
    : never;

/** DeclaredGoaError is the union of the errors an error table declares. */
export type DeclaredGoaError<T extends ErrorTable> = {
  [S in keyof T]: T[S] extends readonly (infer E)[] ? EntryError<E> : never;
}[keyof T];

/**
 * goaErrorDecoder turns a non-success response into a typed error. The goa
 * error name is taken from the `goa-error` header, then the body, then the
 * table when the status declares a single error.
 *
 * A response that goa did not produce (no `goa-error` header and a body that
 * is not a goa error) is an `UnexpectedResponseError`, even when its status is
 * one the design declares: a proxy or middleware in front of the service may
 * have answered.
 */
export function goaErrorDecoder<const T extends ErrorTable>(
  table: T,
): (
  response: Response,
) => Promise<
  DeclaredGoaError<T> | GoaError<OtherErrorName> | UnexpectedResponseError | ResponseValidationError
> {
  type Out = DeclaredGoaError<T> | GoaError<OtherErrorName> | UnexpectedResponseError | ResponseValidationError;
  return async (response) => {
    const text = await response.text();
    const contentType = response.headers.get("content-type") ?? "";
    let body: unknown = text;
    let parsed = false;
    if (/\bjson\b|vnd\.goa\.error/i.test(contentType)) {
      try {
        body = JSON.parse(text);
        parsed = true;
      } catch {
        // Not JSON after all: fall through with the text body.
      }
    }
    const goa = parsed ? asErrorResult(body) : undefined;
    const entries = table[response.status] ?? [];
    const header = response.headers.get("goa-error");
    const name = header ?? goa?.name ?? (entries.length === 1 ? entries[0]!.name : undefined);
    const entry = name === undefined ? undefined : entries.find((e) => e.name === name);

    if (entry?.schema) {
      if (entry.headers && (isRecord(body) || text === "")) {
        // The attributes mapped to headers are missing from the body.
        const merged: Record<string, unknown> = isRecord(body) ? { ...body } : {};
        readHeaders(merged, response.headers, entry.headers);
        body = merged;
      } else if (!parsed) {
        return new UnexpectedResponseError(response, text);
      }
      const detail = decode(entry.schema, body);
      if (!detail.ok) {
        return detail.error;
      }
      const message =
        isRecordWithMessage(body) ? body.message : `${entry.name}: HTTP ${response.status}`;
      return new GoaError({ name: entry.name, message, body, response, detail: detail.value }) as Out;
    }
    if (goa && name !== undefined) {
      return new GoaError({
        name,
        message: goa.message,
        id: goa.id,
        temporary: goa.temporary,
        timeout: goa.timeout,
        fault: goa.fault,
        body,
        response,
        detail: undefined,
      }) as Out;
    }
    if (entry && header === entry.name) {
      // goa named a declared error whose body is neither its default error
      // nor a type this client has a schema for.
      const message = isRecordWithMessage(body) ? body.message : `${entry.name}: HTTP ${response.status}`;
      return new GoaError({ name: entry.name, message, body, response, detail: undefined }) as Out;
    }
    return new UnexpectedResponseError(response, text);
  };
}

/** ResponseTableEntry says how one success response carries the result. */
export interface ResponseTableEntry {
  /**
   * body is the part of the result found in the response body: "fields" is a
   * JSON object of attributes, "attr" the JSON of one attribute, "whole" the
   * JSON of the result itself and "none" nothing. A "raw" body is not read: it
   * is left on the Response for the caller.
   */
  readonly body: "fields" | "attr" | "whole" | "none" | "raw";
  /** attr is the JSON key of the attribute an "attr" body holds. */
  readonly attr?: string;
  /**
   * nullAs is what a "whole" body of `null` stands for. A Go service encodes a
   * nil slice or map that way.
   */
  readonly nullAs?: "array" | "map";
  /**
   * scalar is true when the result is not an object: the single header, or
   * the body, is then the value itself. The header is listed under the key "".
   */
  readonly scalar?: boolean;
  /** headers are the result attributes that travel in response headers. */
  readonly headers?: ResponseHeaders;
  /** tag is the attribute, and its value, that identify this response. */
  readonly tag?: readonly [attr: string, value: string];
}

/** ResponseTable lists an endpoint's success responses by HTTP status code. */
export type ResponseTable = Readonly<Record<number, ResponseTableEntry>>;

type DecodeFailure = ResponseValidationError | UnexpectedResponseError | TransportError;

/**
 * decodeResponse turns the response of a unary endpoint into the function's
 * result. A status missing from the table goes to `decodeError`. Otherwise the
 * result is assembled from the body, the mapped headers and the tag of that
 * response, then decoded and validated with `schema`. A null schema means the
 * method has no result.
 *
 * It never throws: a body that cannot be read is a `NetworkError` or an
 * `AbortError`.
 */
export function decodeResponse<E>(
  response: Response,
  schema: null,
  responses: ResponseTable,
  decodeError: (response: Response) => Promise<E>,
): Promise<CallResult<void, E | DecodeFailure>>;
export function decodeResponse<T extends z.ZodType, E>(
  response: Response,
  schema: T,
  responses: ResponseTable,
  decodeError: (response: Response) => Promise<E>,
): Promise<CallResult<z.output<T>, E | DecodeFailure>>;
export async function decodeResponse<E>(
  response: Response,
  schema: z.ZodType | null,
  responses: ResponseTable,
  decodeError: (response: Response) => Promise<E>,
): Promise<CallResult<unknown, E | DecodeFailure>> {
  const fail = <F>(error: F): CallResult<never, F> => ({ ok: false, error, response });
  try {
    const entry = Object.hasOwn(responses, response.status) ? responses[response.status] : undefined;
    if (!entry) {
      return fail(await decodeError(response));
    }

    let wire: unknown;
    switch (entry.body) {
      case "fields":
      case "attr":
      case "whole": {
        const text = await response.text();
        if (!isJSONContentType(response.headers.get("content-type"))) {
          return fail(new UnexpectedResponseError(response, text));
        }
        let json: unknown;
        try {
          json = JSON.parse(text);
        } catch (cause) {
          return fail(invalidJSON(text, cause));
        }
        if (entry.body === "attr") {
          // goa writes null for an unset attribute.
          wire = json === null || entry.attr === undefined ? {} : { [entry.attr]: json };
        } else if (entry.body === "whole" && json === null && entry.nullAs) {
          wire = entry.nullAs === "array" ? [] : {};
        } else {
          wire = json;
        }
        break;
      }
      case "none":
        // Nothing to read; release the connection.
        void response.body?.cancel().catch(() => undefined);
        wire = entry.scalar ? undefined : {};
        break;
      case "raw":
        wire = entry.scalar ? undefined : {};
        break;
    }

    if (schema === null) {
      return { ok: true, value: undefined, response };
    }
    if (entry.scalar) {
      const header = entry.headers?.[""];
      const value = header ? response.headers.get(header.name) : null;
      if (header && value !== null) {
        wire = parseHeader(value, header.shape);
      }
    } else if (isRecord(wire)) {
      if (entry.headers) {
        readHeaders(wire, response.headers, entry.headers);
      }
      if (entry.tag) {
        wire[entry.tag[0]] = entry.tag[1];
      }
    }

    const decoded = decode(schema, wire);
    if (!decoded.ok) {
      return fail(decoded.error);
    }
    return { ok: true, value: decoded.value, response };
  } catch (cause) {
    if (isAbort(cause)) {
      return fail(new AbortError(undefined, { cause }));
    }
    return fail(new NetworkError(cause instanceof Error ? cause.message : "network error", { cause }));
  }
}

/** isJSONContentType reports whether a Content-Type is JSON: `application/json` or a `+json` suffix. */
export function isJSONContentType(contentType: string | null): boolean {
  const mediaType = (contentType ?? "").split(";")[0]!.trim().toLowerCase();
  return mediaType === "application/json" || mediaType.endsWith("+json");
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isRecordWithMessage(value: unknown): value is { message: string } {
  return typeof value === "object" && value !== null && typeof (value as { message?: unknown }).message === "string";
}

function base64ToBytes(text: string): Uint8Array {
  const binary = atob(text);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    out[i] = binary.charCodeAt(i);
  }
  return out;
}

/** bytesToBase64 encodes bytes as padded standard base64, as Go's encoding/json does. */
export function bytesToBase64(value: Uint8Array): string {
  let binary = "";
  const chunk = 0x8000;
  for (let i = 0; i < value.length; i += chunk) {
    binary += String.fromCharCode(...value.subarray(i, i + chunk));
  }
  return btoa(binary);
}
