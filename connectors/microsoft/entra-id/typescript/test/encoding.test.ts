// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { encodeGraphQuery, pathEscape } from '../src/client.js';

const goEncodingCases = [
  { input: 'hello world', query: 'hello%20world', path: 'hello%20world' },
  { input: "~*!'()", query: '~%2A%21%27%28%29', path: '~%2A%21%27%28%29' },
  { input: '$&+=:@/', query: '%24%26%2B%3D%3A%40%2F', path: '$&+=:@%2F' },
  { input: 'café', query: 'caf%C3%A9', path: 'caf%C3%A9' },
];

it.each(goEncodingCases)('matches Go net/url encoding for $input', ({ input, query, path }) => {
  expect(encodeGraphQuery({ '$value': input })).toBe(`$value=${query}`);
  expect(pathEscape(input)).toBe(path);
});

it('sorts literal OData keys in Go byte order', () => {
  expect(encodeGraphQuery({ '$top': '10', '$filter': 'x y', '$count': 'true' })).toBe('$count=true&$filter=x%20y&$top=10');
});
