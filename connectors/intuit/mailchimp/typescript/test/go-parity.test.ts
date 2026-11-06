// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { describe, expect, it } from 'vitest';
import { AddressRefusal } from '@superdurable/connectors-sdk/providerhttp';
import type { Call } from '@superdurable/connectors-sdk';
import { goRFC3339UTC, goSimpleLower, goTrimSpace, isGoBareEmailAddress } from '../src/go-parity.js';
import { ListMembersInputSchema } from '../src/index.js';
import { MailchimpClient } from '../src/client.js';

// Expected values were produced by Go 1.24.13 net/mail, strings, and time.
describe('Go standard-library parity', () => {
  it.each([
    ['jane@ex_ample.com', true], ['jane@-ex.com', true], ['jane@ex-.com', true],
    ['jane@a+b.com', true], ["jane@a'b.com", true], ['jane@a_b', true],
    ['jane@é.com', true], ['jane@\ufeff.com', true], ['jane@[127.0.0.1]', true],
    ['jane@[a]', false], ['jane@[x]', false], ['jane@[1]', false],
    ['jane@[IPv6:::1]', false], ['=@[;]', false], ['"jane"@example.com', false],
    ['jane@example.com (c)', false],
  ] as const)('net/mail bare address %s', (address, valid) => {
    expect(isGoBareEmailAddress(address)).toBe(valid);
  });

  it.each([
    ['2026-10-25T10:00:00Z', '2026-10-25T10:00:00Z'],
    ['2026-10-25T1:00:00Z', '2026-10-25T01:00:00Z'],
    ['2026-10-25T10:00:00+24:00', '2026-10-24T10:00:00Z'],
    ['2026-10-25T10:00:00+23:60', '2026-10-24T10:00:00Z'],
    ['2026-10-25T10:00:00+01:60', '2026-10-25T08:00:00Z'],
    ['2026-10-25T10:00:00.0000000001Z', '2026-10-25T10:00:00Z'],
    ['2026-10-25T10:00:00,123Z', '2026-10-25T10:00:00.123Z'],
    ['0001-01-01T00:00:00Z', '0001-01-01T00:00:00Z'],
  ] as const)('time.Parse RFC3339 %s', (input, expected) => {
    expect(goRFC3339UTC(input)).toBe(expected);
  });
  it.each([
    '2026-10-25T10:00:00+25:00', '2026-10-25T10:00:00+00:99',
    '2026-10-25T10:00:00+1:00', '2026-10-25T10:00:00+00:0',
  ])('time.Parse rejects %s', input => {
    expect(() => goRFC3339UTC(input)).toThrow();
  });

  it.each([
    ['\u0085tag', 'tag'], ['\ufefftag', '\ufefftag'],
    ['\u00a0tag', 'tag'], ['\u200btag', '\u200btag'],
  ] as const)('strings.TrimSpace %s', (input, expected) => {
    expect(goTrimSpace(input)).toBe(expected);
  });
  it.each([
    ['ΟΔΥΣΣΕΑΣ@example.gr', 'οδυσσεασ@example.gr'],
    ['İ@x.com', 'i@x.com'],
  ] as const)('strings.ToLower %s', (input, expected) => {
    expect(goSimpleLower(input)).toBe(expected);
    expect(createHash('md5').update(goSimpleLower(input)).digest('hex'))
      .toBe(createHash('md5').update(expected).digest('hex'));
  });
});

it('accepts Go RFC 3339 offset forms through the public list schema', () => {
  expect(ListMembersInputSchema.parse({ listId: '57afe96172', changedSince: '2026-10-25T10:00:00+01:60' }).changedSince)
    .toBe('2026-10-25T10:00:00+01:60');
});

it('classifies address-policy refusal as a request that was never sent', async () => {
  const client = new MailchimpClient({}, { resolve: async () => { throw new Error('unused'); } },
    { apiBaseURL: 'http://127.0.0.1:12345/3.0', send: async () => { throw new AddressRefusal('refused before request'); } });
  const call = { context: { cancellationSignal: new AbortController().signal } } as Call;
  const exchange = await client.exchange(call, { key: 'TESTKEY1-us6', baseURL: 'http://127.0.0.1:12345/3.0' },
    'sendCampaign', 'POST', '/campaigns/42694e9e57/actions/send');
  expect(exchange.outcome).toBe('notSent');
});
