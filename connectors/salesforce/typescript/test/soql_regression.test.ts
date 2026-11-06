// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { renderSOQL } from '../src/soql.js';

it('rejects an impossible Gregorian dateTime instead of rolling it into March', () => {
  expect(() => renderSOQL('SELECT Id FROM Account WHERE CreatedDate = :when',
    { when: { kind: 'dateTime', text: '2026-02-29T12:00:00Z' } })).toThrow(/dateTime/);
});

it.each([
  ['2026-01-01T1:00:00Z', '2026-01-01T01:00:00Z'],
  ['2026-01-01T12:00:00,5Z', '2026-01-01T12:00:00Z'],
  ['2026-01-01T12:00:00+24:00', '2025-12-31T12:00:00Z'],
])('accepts Go RFC3339 form %s', (input, expected) => {
  expect(renderSOQL('SELECT Id FROM Account WHERE CreatedDate = :when',
    { when: { kind: 'dateTime', text: input } })).toContain(expected);
});

it('reports a validation defect for a lone surrogate', () => {
  expect(() => renderSOQL('SELECT Id FROM Account WHERE Name = :name',
    { name: { kind: 'string', text: '\uD800' } })).toThrow(/valid UTF-8/);
});

it('accepts Go year-zero RFC3339 timestamps', () => {
  expect(renderSOQL('SELECT Id FROM Account WHERE CreatedDate = :when',
    { when: { kind: 'dateTime', text: '0000-01-01T00:00:00Z' } })).toContain('0000-01-01T00:00:00Z');
});

it('uses the same unrestricted request transport as the Go Salesforce client', async () => {
  const { SalesforceClient } = await import('../src/client.js');
  const { SecretString } = await import('@superdurable/connectors-sdk');
  const credentials = { authMethodId: 'salesforce-oauth', oauthClientId: 'client',
    accessToken: new SecretString('token'), instanceUrl: 'https://example.com' };
  let policy = '';
  const client = new SalesforceClient({}, { resolve: async () => credentials,
    resolveWithRefresh: async () => credentials }, { send: async request => {
      policy = request.addressPolicy ?? '';
      return { status: 200, headers: {}, body: Buffer.from('{}') };
    } });
  await client.exchange({ id: 'call', context: { cancellationSignal: new AbortController().signal } } as never,
    credentials, 'getRecord', { method: 'GET', path: '/services/data/v62.0/sobjects/Account/record' });
  expect(policy).toBe('unrestricted');
});
