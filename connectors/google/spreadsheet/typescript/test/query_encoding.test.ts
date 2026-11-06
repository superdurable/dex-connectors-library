// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { goQueryEncode } from '../src/client.js';

describe('Go url.Values.Encode agreement', () => {
  it('sorts UTF-8 keys and retains value order with Go query escaping', () => {
    expect(goQueryEncode({ z: ['two', 'one'], a: "~*!'() +/?&=中", 'é': 'é' })).toBe('a=~%2A%21%27%28%29+%2B%2F%3F%26%3D%E4%B8%AD&z=two&z=one&%C3%A9=%C3%A9');
  });
});
