// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { goQueryEscape } from '../src/operations.js';

describe('Go url.QueryEscape agreement', () => {
  it.each([
  ['~', '~'],
  ['*', '%2A'],
  ['!', '%21'],
  ["'", '%27'],
  ['(', '%28'],
  [')', '%29'],
  [' ', '+'],
  ['+', '%2B'],
  ['/', '%2F'],
  ['?', '%3F'],
  ['&', '%26'],
  ['=', '%3D'],
  ['é', '%C3%A9'],
  ['中', '%E4%B8%AD']
  ])('encodes %s', (input, encoded) => {
    expect(goQueryEscape(input)).toBe(encoded);
  });
});
