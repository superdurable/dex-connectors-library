// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString } from '@superdurable/connectors-sdk';
import { isBareEmail, ReamazeClient } from '../src/client.js';
import { parseInstant, pathEscape } from '../src/wire.js';

it('matches Re:amaze email and path boundaries used by Go', () => {
  for (const valid of ["sean.o'brien@example.ie", 'é@exämple.com', 'a@localhost', 'a!b@example.com']) expect(isBareEmail(valid)).toBe(true);
  for (const invalid of ['.a@example.com', 'a.@example.com', 'a..b@example.com', 'a@ex..ample.com', 'a@[127.0.0.1]', 'Jane <jane@example.com>']) expect(isBareEmail(invalid)).toBe(false);
  expect(pathEscape("a!b$c&d+e=f:g@h,i;j?k/l")).toBe('a%21b$c&d+e=f:g@h%2Ci%3Bj%3Fk%2Fl');
});

it('rejects impossible civil dates and accepts Go RFC 3339 fractions and offsets', () => {
  for (const invalid of ['2026-02-30T00:00:00Z', '2025-02-29T00:00:00Z', '2026-01-01T24:00:00Z']) expect(() => parseInstant(invalid)).toThrow();
  expect(parseInstant('2024-02-29T12:00:00,125+99:59')).toContain('.125Z');
});

it('honors a configured response bound before sending', () => {
  const credentials = { resolve: async () => ({ email: 'agent@example.com', apiToken: new SecretString('testToken') }) };
  expect(() => new ReamazeClient({ brand: 'acme', maxResponseBytes: 0 }, credentials)).toThrow();
  expect(() => new ReamazeClient({ brand: 'acme', maxResponseBytes: 1024 }, credentials)).not.toThrow();
});
