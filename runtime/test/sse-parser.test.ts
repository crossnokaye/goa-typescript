import { describe, expect, it } from "vitest";

import { createParser, type SseFrame } from "../src/index.js";

function parse(...chunks: string[]): { frames: SseFrame[]; retries: number[]; ids: string[] } {
  const frames: SseFrame[] = [];
  const retries: number[] = [];
  const ids: string[] = [];
  const parser = createParser(
    (frame) => frames.push(frame),
    (retry) => retries.push(retry),
    (id) => ids.push(id),
  );
  for (const chunk of chunks) {
    parser.feed(chunk);
  }
  return { frames, retries, ids };
}

describe("createParser", () => {
  const table: { name: string; input: string; want: SseFrame[] }[] = [
    { name: "single data line", input: "data: hello\n\n", want: [{ data: "hello" }] },
    { name: "no space after colon", input: "data:hello\n\n", want: [{ data: "hello" }] },
    { name: "only one leading space is stripped", input: "data:  hello\n\n", want: [{ data: " hello" }] },
    { name: "multi-line data joins with LF", input: "data: a\ndata: b\n\n", want: [{ data: "a\nb" }] },
    { name: "empty data line is kept", input: "data\n\n", want: [{ data: "" }] },
    { name: "empty data lines join", input: "data\ndata\n\n", want: [{ data: "\n" }] },
    { name: "CRLF line endings", input: "data: a\r\ndata: b\r\n\r\n", want: [{ data: "a\nb" }] },
    { name: "CR line endings", input: "data: a\rdata: b\r\r", want: [{ data: "a\nb" }] },
    { name: "mixed line endings", input: "data: a\r\ndata: b\rdata: c\n\n", want: [{ data: "a\nb\nc" }] },
    { name: "comments are ignored", input: ": keep-alive\ndata: x\n: another\n\n", want: [{ data: "x" }] },
    { name: "comment-only block dispatches nothing", input: ": ping\n\n", want: [] },
    { name: "unknown fields are ignored", input: "foo: bar\ndata: x\n\n", want: [{ data: "x" }] },
    { name: "field names are case sensitive", input: "Data: x\n\n", want: [] },
    { name: "event without data is not dispatched", input: "event: tick\n\n", want: [] },
    { name: "event type", input: "event: tick\ndata: 1\n\n", want: [{ data: "1", event: "tick" }] },
    {
      name: "event type does not carry over",
      input: "event: tick\ndata: 1\n\ndata: 2\n\n",
      want: [{ data: "1", event: "tick" }, { data: "2" }],
    },
    {
      name: "event type set without data is reset",
      input: "event: tick\n\ndata: 2\n\n",
      want: [{ data: "2" }],
    },
    { name: "id", input: "id: 7\ndata: x\n\n", want: [{ data: "x", id: "7" }] },
    {
      name: "id persists across events",
      input: "id: 7\ndata: a\n\ndata: b\n\n",
      want: [{ data: "a", id: "7" }, { data: "b", id: "7" }],
    },
    {
      name: "empty id resets the last event ID",
      input: "id: 7\ndata: a\n\nid\ndata: b\n\n",
      want: [{ data: "a", id: "7" }, { data: "b", id: "" }],
    },
    { name: "id containing NUL is ignored", input: "id: a\u0000b\ndata: x\n\n", want: [{ data: "x" }] },
    { name: "retry", input: "retry: 1500\ndata: x\n\n", want: [{ data: "x", retry: 1500 }] },
    { name: "non-digit retry is ignored", input: "retry: 1.5\ndata: x\n\n", want: [{ data: "x" }] },
    { name: "empty retry is ignored", input: "retry:\ndata: x\n\n", want: [{ data: "x" }] },
    {
      name: "retry does not carry over",
      input: "retry: 10\ndata: a\n\ndata: b\n\n",
      want: [{ data: "a", retry: 10 }, { data: "b" }],
    },
    { name: "colon in value", input: "data: a: b\n\n", want: [{ data: "a: b" }] },
    { name: "leading BOM is stripped", input: "﻿data: x\n\n", want: [{ data: "x" }] },
    { name: "partial event at end of input is discarded", input: "data: a\n\ndata: b\n", want: [{ data: "a" }] },
    { name: "unterminated line at end of input is discarded", input: "data: a", want: [] },
    { name: "JSON payload", input: 'data: {"a":1}\n\n', want: [{ data: '{"a":1}' }] },
  ];

  for (const tc of table) {
    it(tc.name, () => {
      expect(parse(tc.input).frames).toEqual(tc.want);
    });
    it(`${tc.name} (one character per chunk)`, () => {
      expect(parse(...tc.input).frames).toEqual(tc.want);
    });
  }

  it("treats CR at a chunk boundary followed by LF as one line ending", () => {
    expect(parse("data: a\r", "\ndata: b\r", "\n\r", "\n").frames).toEqual([{ data: "a\nb" }]);
  });

  it("treats CR at a chunk boundary followed by text as a complete line ending", () => {
    expect(parse("data: a\r", "data: b\r", "\r").frames).toEqual([{ data: "a\nb" }]);
  });

  it("dispatches as soon as the blank line arrives", () => {
    const frames: SseFrame[] = [];
    const parser = createParser((frame) => frames.push(frame));
    parser.feed("data: a\n");
    expect(frames).toEqual([]);
    parser.feed("\n");
    expect(frames).toEqual([{ data: "a" }]);
  });

  it("reports retry and id as they are read", () => {
    const got = parse("retry: 250\nid: 9\n\n");
    expect(got.frames).toEqual([]);
    expect(got.retries).toEqual([250]);
    expect(got.ids).toEqual(["9"]);
  });

  it("does not strip a BOM after the first chunk", () => {
    expect(parse("data: a\n\n", "﻿data: b\n\n").frames).toEqual([{ data: "a" }]);
  });
});
