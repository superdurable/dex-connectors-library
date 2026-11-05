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

it('keeps provider message text out of failures and selects safe detail tokens', async () => {
  const body = Buffer.from(JSON.stringify({ error: 'Invalid search: customer jane@x.com secret words', details: { base: [{ type: 'some text here', error: 'SafeCode' }] } }));
  const client = new ZendeskClient({ subdomain: 'acme' }, credentials, { send: async () => ({ status: 422, headers: {}, body }) });
  const result = await client.exchange(call, { email: 'agent@example.com', apiToken: 'zendeskTestToken' }, 'searchTickets', 'GET', '/search/export');
  expect(result.outcome).toBe('rejected');
  if (result.outcome === 'rejected') expect(result.failure.message).toBe('Zendesk rejected the request (HTTP 422) [details: base=SafeCode]');
});

it('reports the first five detail fields in Go byte order', async () => {
  const body = Buffer.from(JSON.stringify({ details: { a_: [{}], a1: [{}], a2: [{}], a3: [{}], a4: [{}], a5: [{}] } }));
  const client = new ZendeskClient({ subdomain: 'acme' }, credentials, { send: async () => ({ status: 422, headers: {}, body }) });
  const result = await client.exchange(call, { email: 'agent@example.com', apiToken: 'zendeskTestToken' }, 'searchTickets', 'GET', '/search/export');
  expect(result.outcome).toBe('rejected');
  if (result.outcome === 'rejected') expect(result.failure.message).toBe('Zendesk rejected the request (HTTP 422) [details: a1, a2, a3, a4, a5]');
});

it('escapes Go query bytes and replaces a lone surrogate without throwing', async () => {
  let requestURL = '';
  const client = new ZendeskClient({ subdomain: 'acme' }, credentials, { send: async request => { requestURL = request.url; return { status: 200, headers: {}, body: Buffer.from('{}') }; } });
  const result = await client.exchange(call, { email: 'agent@example.com', apiToken: 'zendeskTestToken' }, 'searchTickets', 'GET', '/search/export', { query: 'a~b !\uD800' });
  expect(result.outcome).toBe('success');
  expect(requestURL).toContain('query=a~b+%21%EF%BF%BD');
});
