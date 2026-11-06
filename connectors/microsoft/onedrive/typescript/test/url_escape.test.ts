// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { pathEscape, queryEscape } from '../src/client.js';

// Recorded from Go net/url.QueryEscape and PathEscape.
it.each([
  ['a b~*!\'()', 'a+b~%2A%21%27%28%29', 'a%20b~%2A%21%27%28%29'],
  ['a/b;c,d?e', 'a%2Fb%3Bc%2Cd%3Fe', 'a%2Fb%3Bc%2Cd%3Fe'],
  ['é &+%', '%C3%A9+%26%2B%25', '%C3%A9%20&+%25'],
  ['$select=id,name', '%24select%3Did%2Cname', '$select=id%2Cname'],
  ["O'Neil", 'O%27Neil', 'O%27Neil'],
  ['b!team', 'b%21team', 'b%21team'],
])('uses Go URL escaping for %s', (value, query, path) => {
  expect(queryEscape(value)).toBe(query);
  expect(pathEscape(value)).toBe(path);
});
