// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { isBareEmailAddress, ZendeskClient } from '../src/client.js';
import { buildTicketSearchQuery } from '../src/operations.js';

const validEmail = ['agent@example.com', 'é@example.com', 'a@例子.测试', 'agent@[127.0.0.1]', 'agent@[::1]', 'a@-bad.com', 'a@foo_bar.com'];
const invalidEmail = ['Agent <agent@example.com>', 'a@bad..com', 'a@[abc]', 'a@[IPv6:::1]', 'a:b@example.com', 'a@example.com status:solved'];

describe('Go-compatible input validation', () => {
  it.each(validEmail)('accepts a bare address: %s', value => expect(isBareEmailAddress(value)).toBe(true));
  it.each(invalidEmail)('rejects a non-bare address: %s', value => expect(isBareEmailAddress(value)).toBe(false));
  it.each(['2026-02-30T12:00:00Z', '2025-02-29T12:00:00Z', '2026-01-01T24:00:00Z', '2026-01-01T12:60:00Z'])(
    'rejects an impossible RFC 3339 instant: %s', value => expect(() => buildTicketSearchQuery({ updatedSince: value })).toThrow(),
  );
  it('accepts Go offset ranges and comma fractions', () => {
    expect(buildTicketSearchQuery({ updatedSince: '2024-02-29T12:00:00,123456789+99:59' })).toBe('updated>=2024-02-25T08:01:00.123456789Z');
  });
  it('validates subdomain and response limits at construction', () => {
    const credentials = { resolve: async () => { throw new Error('unused'); } };
    expect(() => new ZendeskClient({ subdomain: 'Bad' }, credentials)).toThrow();
    expect(() => new ZendeskClient({ subdomain: 'acme', maxResponseBytes: 0 }, credentials)).toThrow();
  });
});
