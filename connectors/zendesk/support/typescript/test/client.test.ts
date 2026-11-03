// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ZendeskClient } from '../src/client.js';

const credentials = { resolve: async () => ({ email: 'agent@example.com', apiToken: new SecretString('zendeskTestToken') }) };
const call = { id: 'test-call', context: { cancellationSignal: new AbortController().signal } } as Call;

it('classifies a large rate-limit response by status', async () => {
  const client = new ZendeskClient({ subdomain: 'acme', maxResponseBytes: 16 }, credentials, {
    send: async () => { throw new BodyTooLargeError(429, { 'retry-after': '4' }); },
  });
  const result = await client.exchange(call, { email: 'agent@example.com', apiToken: 'zendeskTestToken' }, 'searchTickets', 'GET', '/search/export');
  expect(result.outcome).toBe('retry');
  if (result.outcome === 'retry') { expect(result.failure.kind).toBe('RATE_LIMIT'); expect(result.retryAfterMs).toBe(4000); }
});

it('retries a local transport refusal without exposing credentials', async () => {
  const client = new ZendeskClient({ subdomain: 'acme' }, credentials, { send: async () => { throw new ProviderTransportError(true); } });
  const result = await client.exchange(call, { email: 'agent@example.com', apiToken: 'zendeskTestToken' }, 'getTicket', 'GET', '/tickets/1');
  expect(result.outcome).toBe('retry');
  if (result.outcome === 'retry') { expect(result.failure.kind).toBe('TRANSPORT'); expect(result.failure.message).not.toContain('zendeskTestToken'); }
});
