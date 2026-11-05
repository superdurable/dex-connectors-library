// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { encodeGoQuery } from '../src/client.js';

// Expected strings were produced with net/url.Values.Encode.
it.each([
  [[['z', 'a b~!*'], ['é', 'value'], ['z', 'second']], 'z=a+b~%21%2A&z=second&%C3%A9=value'],
  [[['a*b', 'x+y/z%'], ['a*b', "! '()~"]], 'a%2Ab=x%2By%2Fz%25&a%2Ab=%21+%27%28%29~'],
  [[['ключ', '🐱 ~*']], '%D0%BA%D0%BB%D1%8E%D1%87=%F0%9F%90%B1+~%2A'],
] as const)('matches Go url.Values.Encode for %j', (pairs, expected) => {
  expect(encodeGoQuery(new URLSearchParams(pairs.map(([key, value]) => [key, value])))).toBe(expected);
});
