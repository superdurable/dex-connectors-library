// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { parseGraphTime, pathEscape, queryEncode } from '../src/message.js';

it('matches Go url.Values.Encode for sorted keys and form escaping', () => {
  expect(queryEncode({ '$top': '50' })).toBe('%24top=50');
  expect(queryEncode({ 'space key': 'a b', '!': "!*'()~", '~': '~ *' }))
    .toBe('%21=%21%2A%27%28%29~&space+key=a+b&~=~+%2A');
  expect(queryEncode({ 'é': '你好 *~!', a: 'a+b/c?d' }))
    .toBe('a=a%2Bb%2Fc%3Fd&%C3%A9=%E4%BD%A0%E5%A5%BD+%2A~%21');
});

it('matches Go url.PathEscape', () => {
  expect(pathEscape('19:abc@thread.tacv2')).toBe('19:abc@thread.tacv2');
  expect(pathEscape("a b!*'()~")).toBe('a%20b%21%2A%27%28%29~');
  expect(pathEscape('你好/yes')).toBe('%E4%BD%A0%E5%A5%BD%2Fyes');
  expect(pathEscape("-._~:/@!$&'()*+,;=?#[]")).toBe('-._~:%2F@%21$&%27%28%29%2A+%2C%3B=%3F%23%5B%5D');
});

it('keeps Go RFC3339Nano precision and rejects hour 24 clock values', () => {
  expect(parseGraphTime('2026-10-01T09:30:01.123456789Z', 'createdDateTime'))
    .toBe('2026-10-01T09:30:01.123456789Z');
  expect(parseGraphTime('2026-10-01T09:30:01.123456789+01:00', 'createdDateTime'))
    .toBe('2026-10-01T08:30:01.123456789Z');
  expect(parseGraphTime('0001-01-01T00:00:00Z', 'createdDateTime')).toBe('0001-01-01T00:00:00Z');
  expect(() => parseGraphTime('2026-10-01T24:00:00Z', 'createdDateTime')).toThrow();
});

it('keeps Go year zero and provider token limits', async () => {
  const { decodeMessage } = await import('../src/message.js');
  expect(parseGraphTime('0000-01-01T00:00:00Z', 'createdDateTime')).toBe('0000-01-01T00:00:00Z');
  const message = decodeMessage({ id: '1', createdDateTime: '2026-01-01T00:00:00Z',
    messageType: 'x'.repeat(65), importance: 'x'.repeat(65),
    from: { user: { id: 'sender', displayName: 'A\u200dB\u0085C' } },
    body: { contentType: 'x'.repeat(65), content: 'text' } });
  expect(message.messageType).toBe('');
  expect(message.importance).toBeUndefined();
  expect(message.contentType).toBeUndefined();
  expect(message.sender.displayName).toBe('A\u200dBC');
});
