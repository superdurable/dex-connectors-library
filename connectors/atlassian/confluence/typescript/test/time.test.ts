// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { parseGoRFC3339 } from '../src/time.js';

it.each(['2026-02-30T12:00:00Z', '2025-02-29T12:00:00Z', '2026-01-01T24:00:00Z', '2026-01-01T12:60:00Z', '2026-01-01T12:00:60Z', '2026-01-01T12:00:00+01:60'])('rejects invalid RFC3339 calendar time %s', value => {
  expect(parseGoRFC3339(value)).toBeUndefined();
});
it('accepts leap day, comma fractions, and Go-compatible wide offsets', () => {
  expect(parseGoRFC3339('2024-02-29T23:59:59,123000Z')?.canonical).toBe('2024-02-29T23:59:59.123Z');
  expect(parseGoRFC3339('2026-01-01T00:00:00+99:59')?.canonical).toBe('2026-01-01T00:00:00+99:59');
  expect(parseGoRFC3339('0001-01-01T00:00:00Z')?.canonical).toBe('0001-01-01T00:00:00Z');
});
