import { describe, expect, it } from 'vitest';

import { appendMapQuery, appendQuery, buildURL, encodeHeaderValue, encodePathParam, jsonBody } from '../src/index.js';

function query(build: (params: URLSearchParams) => void): string {
  const params = new URLSearchParams();
  build(params);
  return params.toString();
}

describe('appendQuery', () => {
  it("formats scalars the way goa's Go client does", () => {
    expect(query((p) => appendQuery(p, 's', 'a b&c'))).toBe('s=a+b%26c');
    expect(query((p) => appendQuery(p, 'n', 42))).toBe('n=42');
    expect(query((p) => appendQuery(p, 'f', 1.5))).toBe('f=1.5');
    expect(query((p) => appendQuery(p, 't', true))).toBe('t=true');
    expect(query((p) => appendQuery(p, 'f', false))).toBe('f=false');
    expect(query((p) => appendQuery(p, 'z', 0))).toBe('z=0');
    expect(query((p) => appendQuery(p, 'e', ''))).toBe('e=');
  });

  it('omits undefined and null', () => {
    expect(query((p) => appendQuery(p, 'a', undefined))).toBe('');
    expect(query((p) => appendQuery(p, 'a', null))).toBe('');
  });

  it('repeats the key for arrays', () => {
    expect(query((p) => appendQuery(p, 'k', ['a', 'b']))).toBe('k=a&k=b');
    expect(query((p) => appendQuery(p, 'k', [1, 2, 3]))).toBe('k=1&k=2&k=3');
    expect(query((p) => appendQuery(p, 'k', []))).toBe('');
  });

  it('brackets map keys, recursively', () => {
    expect(decodeURIComponent(query((p) => appendQuery(p, 'm', { a: 1, b: 'x' })))).toBe('m[a]=1&m[b]=x');
    expect(decodeURIComponent(query((p) => appendQuery(p, 'm', { a: { b: true } })))).toBe('m[a][b]=true');
    expect(decodeURIComponent(query((p) => appendQuery(p, 'm', { a: ['x', 'y'] })))).toBe('m[a]=x&m[a]=y');
  });
});

describe('appendMapQuery', () => {
  it('spreads a map into top-level parameters', () => {
    expect(query((p) => appendMapQuery(p, { a: '1', b: ['x', 'y'] }))).toBe('a=1&b=x&b=y');
    expect(query((p) => appendMapQuery(p, undefined))).toBe('');
  });
});

describe('buildURL', () => {
  it('substitutes and encodes path parameters', () => {
    expect(buildURL('http://h', '/things/{id}/events', { id: 'a b/c?d' })).toBe('http://h/things/a%20b%2Fc%3Fd/events');
    expect(buildURL('http://h', '/n/{n}/b/{b}', { n: 7, b: true })).toBe('http://h/n/7/b/true');
  });

  it('encodes each segment of a wildcard', () => {
    expect(buildURL('http://h', '/files/{*path}', { path: 'a b/c#d/e' })).toBe('http://h/files/a%20b/c%23d/e');
  });

  it('keeps the base URL path prefix and normalizes slashes', () => {
    expect(buildURL('http://h/api/v1', '/things', {})).toBe('http://h/api/v1/things');
    expect(buildURL('http://h/api/v1/', '/things', {})).toBe('http://h/api/v1/things');
    expect(buildURL('http://h', 'things', {})).toBe('http://h/things');
    expect(buildURL('/api', '/things', {})).toBe('/api/things');
    expect(buildURL('', '/things', {})).toBe('/things');
  });

  it('appends the query string only when there is one', () => {
    expect(buildURL('http://h', '/t', {}, new URLSearchParams())).toBe('http://h/t');
    expect(buildURL('http://h', '/t', {}, new URLSearchParams({ a: '1' }))).toBe('http://h/t?a=1');
  });
});

describe('header and body helpers', () => {
  it('joins header arrays with a comma and a space', () => {
    expect(encodeHeaderValue(['a', 'b'])).toBe('a, b');
    expect(encodeHeaderValue([1, 2])).toBe('1, 2');
    expect(encodeHeaderValue(true)).toBe('true');
    expect(encodeHeaderValue('x')).toBe('x');
  });

  it('encodes path parameters and bodies', () => {
    expect(encodePathParam('a/b')).toBe('a%2Fb');
    expect(jsonBody({ a: 1, b: undefined })).toBe('{"a":1}');
  });
});
