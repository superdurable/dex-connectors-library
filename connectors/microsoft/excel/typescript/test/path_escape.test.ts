// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { pathEscape } from '../src/graph.js';

// Values recorded from Go net/url.PathEscape.
it.each([
  ['alpha beta', 'alpha%20beta'], ['a/b', 'a%2Fb'], ["~*!'()", '~%2A%21%27%28%29'],
  ['a+b', 'a+b'], ['a?b', 'a%3Fb'], ['a%b', 'a%25b'], ['a:b', 'a:b'],
  ['a@b', 'a@b'], ['a,b', 'a%2Cb'], ['a;b', 'a%3Bb'], ['a=b', 'a=b'],
  ['a&b', 'a&b'], ['a$b', 'a$b'],
])('matches Go path escaping for %s', (input, expected) => expect(pathEscape(input)).toBe(expected));
