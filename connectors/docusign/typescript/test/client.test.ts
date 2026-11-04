// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { escapePath } from '../src/client.js';
import { parseDocuSignTimestamp } from '../src/operations.js';

describe('Go-compatible DocuSign wire values', () => {
  it('escapes path segments like Go url.PathEscape', () => {
    expect(escapePath("A!B'C(D)E*F,G;H?I/J ~ $&+=:@")).toBe('A%21B%27C%28D%29E%2AF%2CG%3BH%3FI%2FJ%20~%20$&+=:@');
  });
  it('rejects invalid calendar dates and hours', () => {
    for (const value of ['2026-02-30T12:00:00Z', '2025-02-29T12:00:00Z', '2026-10-04T24:00:00Z']) {
      expect(() => parseDocuSignTimestamp(value)).toThrow();
    }
  });
  it('preserves nanoseconds, comma fractions, and large Go offsets', () => {
    expect(parseDocuSignTimestamp('2024-02-29T12:00:00,123456789Z')).toBe('2024-02-29T12:00:00.123456789Z');
    expect(parseDocuSignTimestamp('2026-10-04T12:00:00+99:59')).toBe('2026-09-30T08:01:00Z');
  });
});
