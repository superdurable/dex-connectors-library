// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { encodeGoQuery, queryEscape } from '../src/client.js';
import { goPathEscape } from '../src/operations.js';

describe('Go net/url query encoding', () => {
  it.each([
    ['a b', 'a+b'],
    ["~*!'()", '~%2A%21%27%28%29'],
    ['a/b?c', 'a%2Fb%3Fc'],
    ['é 中文', '%C3%A9+%E4%B8%AD%E6%96%87'],
    ['$&+=:@', '%24%26%2B%3D%3A%40'],
  ])('encodes %s', (value, encoded) => expect(queryEscape(value)).toBe(encoded));

  it('sorts keys by UTF-8 bytes as url.Values.Encode does', () => {
    expect(encodeGoQuery({ '\u{10000}': 'a b', '\uE000': '~*', a: '!' })).toBe('a=%21&%EE%80%80=~%2A&%F0%90%80%80=a+b');
  });
  it.each([
    ['a b', 'a%20b'],
    ["~*!'()", '~%2A%21%27%28%29'],
    ['a/b?c', 'a%2Fb%3Fc'],
    ['é 中文', '%C3%A9%20%E4%B8%AD%E6%96%87'],
    ['$&+=:@', '$&+=:@'],
  ])('matches Go PathEscape for %s', (value, encoded) => expect(goPathEscape(value)).toBe(encoded));
});
