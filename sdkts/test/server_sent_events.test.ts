// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { BodyTooLargeError } from '../src/providerhttp/index.js';
import { ServerSentEventDecoder } from '../src/providerhttp/server_sent_events.js';

it('matches Go event parsing for comments, bare data, empty data, and one leading space', () => {
  const events: { data: string; event?: string }[] = [];
  const decoder = new ServerSentEventDecoder(64);
  decoder.feed(Buffer.from(': keep alive\n\nevent: delta\ndata:  first\ndata:second\n\ndata\n\ndata: \n\n'), event => { events.push(event); });
  expect(events).toEqual([
    { event: 'delta', data: ' first\nsecond' },
    { data: '' },
    { data: '' },
  ]);
});

it('counts field lines but excludes comments from each event limit', () => {
  const decoder = new ServerSentEventDecoder(10);
  expect(() => decoder.feed(Buffer.from(': comment\n: comment\ndata: x\n\n'), () => {})).not.toThrow();
  expect(() => new ServerSentEventDecoder(8).feed(Buffer.from('event: a\ndata: x\n\n'), () => {})).toThrow(BodyTooLargeError);
  expect(() => new ServerSentEventDecoder(8).feed(Buffer.from(': long comment\n\n'), () => {})).toThrow(BodyTooLargeError);
});

it('discards an incomplete trailing event and stops before later data', () => {
  const events: string[] = [];
  const decoder = new ServerSentEventDecoder(64);
  expect(decoder.feed(Buffer.from('data: complete\n\ndata: [DONE]\n\ndata: garbage\n\n'), event => {
    events.push(event.data);
    return event.data !== '[DONE]';
  })).toBe(false);
  expect(events).toEqual(['complete', '[DONE]']);
  const partialEvents: string[] = [];
  new ServerSentEventDecoder(64).feed(Buffer.from('data: unfinished\n'), event => { partialEvents.push(event.data); });
  expect(partialEvents).toEqual([]);
});

it('counts consumed bytes toward the total limit and leaves bytes after DONE unread', () => {
  const stopped = new ServerSentEventDecoder(64, 14);
  expect(stopped.feed(Buffer.from('data: [DONE]\n\ndata: extra bytes\n\n'), event => event.data !== '[DONE]')).toBe(false);
  const overLimit = new ServerSentEventDecoder(64, 13);
  expect(() => overLimit.feed(Buffer.from('data: [DONE]\n\n'), () => true)).toThrow(BodyTooLargeError);
});
