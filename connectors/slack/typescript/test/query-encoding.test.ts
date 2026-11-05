// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { encodeGoQuery } from '../src/client.js';

it('matches Go url.Values.Encode for Unicode ordering and reserved characters', () => {
  const values = new URLSearchParams([['z', 'a b~!*'], ['é', 'value'], ['z', 'second']]);
  expect(encodeGoQuery(values)).toBe('z=a+b~%21%2A&z=second&%C3%A9=value');
});
