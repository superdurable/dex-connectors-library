// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { GoTimeInputSchema } from '../src/timestamps.js';

describe('Go time.Time JSON input', () => {
  it.each([
    '2026-02-03T04:05:06Z',
    '2026-02-03T04:05:06,123456789Z',
    '2026-02-03T04:05:06.123456789123Z',
    '2026-02-03T04:05:06+24:00',
    '2026-02-03T04:05:06+23:60',
    '2026-02-03T04:05:06+24:60',
    '2026-02-03T4:05:06Z',
    '0000-02-29T00:00:00Z',
  ])('accepts and preserves %s', wire => {
    expect(GoTimeInputSchema.parse(wire)).toBe(wire);
  });

  it.each([
    '2026-02-03t04:05:06Z',
    '2026-02-03T04:05:06z',
    '2026-02-03T04:05:06+25:00',
    '2026-02-03T04:05:06+24:61',
    '2026-02-03T04:05:60Z',
    '2026-02-30T04:05:06Z',
    '2026-02-03T04:05:06.Z',
    '2026-02-03T04:05:06Z\n',
  ])('rejects %s', wire => {
    expect(GoTimeInputSchema.safeParse(wire).success).toBe(false);
  });
});
