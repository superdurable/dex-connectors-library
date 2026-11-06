// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { AddressRefusal, parseRetryAfter } from '@superdurable/connectors-sdk/providerhttp';
import { isBareEmail, ReamazeClient } from '../src/client.js';
import { goTrimSpace } from '../src/operations.js';
import { decodeConversation, matchingContacts, parseBody, parseInstant, pathEscape } from '../src/wire.js';

it('matches Re:amaze email and path boundaries used by Go', () => {
  for (const valid of ["sean.o'brien@example.ie", 'é@exämple.com', 'a@localhost', 'a!b@example.com']) expect(isBareEmail(valid)).toBe(true);
  for (const invalid of ['.a@example.com', 'a.@example.com', 'a..b@example.com', 'a@ex..ample.com', 'a@[127.0.0.1]', 'Jane <jane@example.com>']) expect(isBareEmail(invalid)).toBe(false);
  expect(pathEscape("a!b$c&d+e=f:g@h,i;j?k/l")).toBe('a%21b$c&d+e=f:g@h%2Ci%3Bj%3Fk%2Fl');
});

it('matches a Go time.Parse RFC3339 table', () => {
  for (const invalid of ['2026-02-30T00:00:00Z', '2025-02-29T00:00:00Z', '2026-01-01T24:00:00Z', '2024-02-29T12:00:00,125+99:59']) expect(() => parseInstant(invalid)).toThrow();
  for (const [source, expected] of [
    ['2024-02-29T12:00:00,125+24:60', '2024-02-28T11:00:00.125Z'],
    ['2024-02-29T12:00:00,125+00:60', '2024-02-29T11:00:00.125Z'],
    ['2026-01-01T0:00:00Z', '2026-01-01T00:00:00Z'],
    ['2026-01-01T12:30:45.0000000001Z', '2026-01-01T12:30:45Z'],
    ['2026-01-01T12:30:45.1234567899Z', '2026-01-01T12:30:45.123456789Z'],
  ]) expect(parseInstant(source!)).toBe(expected);
});

it('matches Go strings.TrimSpace at the Unicode boundaries', () => {
  for (const [source, expected] of [['\u0085', ''], ['\ufeff', '\ufeff'], [' \u0085Hi\u0085 ', 'Hi'], ['\ufeffHi\ufeff', '\ufeffHi\ufeff'], ['\u200b', '\u200b']]) expect(goTrimSpace(source!)).toBe(expected);
});

it('matches Go providerhttp Retry-After delay parsing', () => {
  const now = new Date('2026-01-01T00:00:00Z');
  for (const [source, expectedMs] of [
    ['7', 7000], ['0', 0], ['-2', 0], ['1.5', 0], ['3601', 3600000],
    ['Thu, 01 Jan 2026 00:00:07 GMT', 7000], ['Thu, 01 Jan 2026 00:00:00 GMT', 0], ['1s', 0], ['', 0],
  ] as const) expect(parseRetryAfter(source, now)).toBe(expectedMs);
});

it('retains the Go json.Number lexeme for numeric origin IDs', () => {
  for (const originId of ['12345678901234567890', '1.0', '1e3']) {
    const wire = parseBody(Buffer.from(`{"origin_id":${originId}}`));
    expect(wire.origin_id).toBe(originId);
  }
});

it('rejects wrong-typed Go wire fields even when the field is not returned', () => {
  const conversation = { slug: 'record', status: 0, created_at: '2026-01-01T00:00:00Z' };
  expect(() => decodeConversation({ ...conversation, message: { body: 7 } }, false)).toThrow();
  expect(() => decodeConversation({ ...conversation, last_customer_message: { body: 7 } }, false)).toThrow();
  expect(() => matchingContacts({ contacts: [{ email: 'other@example.com', name: 7 }], page_count: 1, total_count: 1 }, 'wanted@example.com')).toThrow();
});

it('honors a configured response bound before sending', () => {
  const credentials = { resolve: async () => ({ email: 'agent@example.com', apiToken: new SecretString('testToken') }) };
  expect(() => new ReamazeClient({ brand: 'acme', maxResponseBytes: 0 }, credentials)).toThrow();
  expect(() => new ReamazeClient({ brand: 'acme', maxResponseBytes: 1024 }, credentials)).not.toThrow();
});

it('classifies an address refusal as provably not sent', async () => {
  const credentials = { resolve: async () => ({ email: 'agent@example.com', apiToken: new SecretString('testToken') }) };
  const client = new ReamazeClient({ brand: 'acme' }, credentials, { send: async () => { throw new AddressRefusal('resolved address violates policy'); } });
  const call = { id: 'call', context: {} } as Call;
  const result = await client.exchange(call, 'createConversation', { kind: 'resolved', email: 'agent@example.com', token: 'testToken' }, 'POST', '/conversations', undefined, {});
  expect(result.kind).toBe('notSent');
});

it('matches Go url.Values.Encode for a tilde in an email query', async () => {
  let observedURL = '';
  const credentials = { resolve: async () => ({ email: 'agent@example.com', apiToken: new SecretString('testToken') }) };
  const client = new ReamazeClient({ brand: 'acme' }, credentials, { send: async request => { observedURL = request.url; return { status: 200, headers: {}, body: Buffer.from('{}') }; } });
  await client.exchange({ id: 'call', context: {} } as Call, 'searchConversations', { kind: 'resolved', email: 'agent@example.com', token: 'testToken' }, 'GET', '/conversations', new URLSearchParams({ for: 'a~b@example.com' }));
  expect(observedURL).toContain('for=a~b%40example.com');
});
