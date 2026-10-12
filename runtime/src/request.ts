/**
 * Request encoding helpers. They reproduce what goa's generated Go client
 * sends so a goa server decodes the request the same way.
 */

/** formatScalar renders a primitive the way goa's Go client does. */
function formatScalar(value: unknown): string {
  switch (typeof value) {
    case "string":
      return value;
    case "number":
    case "boolean":
    case "bigint":
      return String(value);
    default:
      return JSON.stringify(value) ?? "";
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/**
 * appendQuery adds one query parameter. Arrays repeat the key, maps become
 * `key[sub]=v` (nested maps nest the brackets) and `undefined`/`null` values
 * are omitted.
 */
export function appendQuery(params: URLSearchParams, key: string, value: unknown): void {
  if (value === undefined || value === null) {
    return;
  }
  if (Array.isArray(value)) {
    for (const item of value) {
      appendQuery(params, key, item);
    }
    return;
  }
  if (isRecord(value)) {
    for (const [sub, item] of Object.entries(value)) {
      appendQuery(params, `${key}[${sub}]`, item);
    }
    return;
  }
  params.append(key, formatScalar(value));
}

/** appendMapQuery adds every entry of a map as its own top-level query parameter. */
export function appendMapQuery(params: URLSearchParams, value: unknown): void {
  if (!isRecord(value)) {
    return;
  }
  for (const [key, item] of Object.entries(value)) {
    appendQuery(params, key, item);
  }
}

/** encodePathParam percent-encodes one path segment. */
export function encodePathParam(value: unknown): string {
  return encodeURIComponent(formatScalar(value));
}

/** encodeHeaderValue renders a header value; arrays are joined with ", ". */
export function encodeHeaderValue(value: unknown): string {
  if (Array.isArray(value)) {
    return value.map(formatScalar).join(", ");
  }
  return formatScalar(value);
}

/** jsonBody serializes a request body. */
export function jsonBody(value: unknown): string {
  return JSON.stringify(value);
}

/**
 * buildURL joins the base URL (keeping its path prefix), the path template
 * with `{name}` and `{*wildcard}` parameters substituted, and the query string.
 * A wildcard keeps its slashes and encodes each segment.
 */
export function buildURL(
  base: string,
  pathTemplate: string,
  pathParams: Record<string, unknown>,
  query?: URLSearchParams,
): string {
  const path = pathTemplate.replace(/\{(\*?)([^}]+)\}/g, (_match, wildcard: string, name: string) => {
    const value = pathParams[name];
    if (wildcard) {
      return formatScalar(value).split("/").map(encodeURIComponent).join("/");
    }
    return encodePathParam(value);
  });
  let url = base.replace(/\/+$/, "") + (path.startsWith("/") ? path : `/${path}`);
  const search = query?.toString();
  if (search) {
    url += (url.includes("?") ? "&" : "?") + search;
  }
  return url;
}
