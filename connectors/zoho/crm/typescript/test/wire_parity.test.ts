// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { encodeGoQuery, escapeGoPath } from '../src/client.js';
import { decodeModuleFields, decodeRecordPage, parseZohoTime } from '../src/records.js';

const goEscapes = [
  ['hello world', 'hello+world', 'hello%20world'],
  ["~*!'()", '~%2A%21%27%28%29', '~%2A%21%27%28%29'],
  ['a/b:c+d&=', 'a%2Fb%3Ac%2Bd%26%3D', 'a%2Fb:c+d&='],
  ['é 🍣', '%C3%A9+%F0%9F%8D%A3', '%C3%A9%20%F0%9F%8D%A3'],
  ['%20', '%2520', '%2520'],
] as const;

it.each(goEscapes)('matches Go url.QueryEscape and PathEscape for %s', (source, query, path) => {
  expect(encodeGoQuery({ x: [source] })).toBe(`x=${query}`);
  expect(escapeGoPath(source)).toBe(path);
});

it('sorts query keys by UTF-8 bytes and keeps repeated values in order', () => {
  expect(encodeGoQuery({ 'z~': ['a b', '*'], 'A!': ["x'y"], 'é': ['🍣'] })).toBe('A%21=x%27y&z~=a+b&z~=%2A&%C3%A9=%F0%9F%8D%A3');
});

it('preserves exact provider numbers in fields and rejects lossy metadata integers', () => {
  const page = decodeRecordPage('Deals', Buffer.from('{"data":[{"id":"123","Amount":12345678901234567890.123456789}]}'));
  expect(JSON.stringify(page.records[0]?.fields)).toBe('{"Amount":12345678901234567890.123456789}');
  for (const token of ['5.0', '1e3', '9007199254740993']) {
    expect(() => decodeModuleFields(Buffer.from(`{"fields":[{"api_name":"Stage","data_type":"text","length":${token}}]}`))).toThrow();
  }
});

it('rejects Go-invalid hour 24 and retains nanosecond precision', () => {
  expect(parseZohoTime('2026-01-28T24:00:00Z')).toBeUndefined();
  expect(parseZohoTime('2026-01-28T13:00:05.123456789Z')).toBe('2026-01-28T13:00:05.123456789Z');
});
