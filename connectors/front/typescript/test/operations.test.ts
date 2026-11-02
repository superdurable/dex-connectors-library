// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { HeartbeatContext } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { FrontClient } from '../src/client.js';
import { buildConversationSearchQuery, buildReplyHTML } from '../src/operations.js';

function call(operationId: string): Call {
  return {
    id: '76e76ff6-9e73-5db4-bb93-69b6a679fae5', connection: { provider: 'front', name: 'front-test' },
    operation: { connectorId: 'front', operationId },
    context: { cancellationSignal: new AbortController().signal } as Call['context'],
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
}

function client(send: (request: ProviderRequest) => Promise<{ status: number; headers: Record<string, string>; body: Uint8Array }>): FrontClient {
  return new FrontClient({}, { resolve: async () => ({ apiToken: new SecretString('test-token') }) },
    { send });
}

it('matches Go path escaping for a contact alias and rejects malformed bare email', async () => {
  const urls: string[] = [];
  const provider = client(async request => { urls.push(request.url); return { status: 404, headers: {}, body: Buffer.from('{}') }; });
  await provider.findContactByEmail().invoke(call('findContactByEmail'), { email: 'jane+tag@acme.example.com' });
  expect(urls).toEqual(['https://api2.frontapp.com/contacts/alt:email:jane+tag@acme.example.com']);
  for (const email of ['.jane@acme.example.com', 'jane..smith@acme.example.com', 'jane@-acme.example.com', 'Jane Smith <jane@acme.example.com>']) {
    const result = await provider.findContactByEmail().invoke(call('findContactByEmail'), { email });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('defect');
  }
  expect(urls).toHaveLength(1);
});

it('retries a provably unsent connection refusal', async () => {
  const provider = client(async () => { throw new ProviderTransportError(true); });
  const result = await provider.findContactByEmail().invoke(call('findContactByEmail'), { email: 'jane@acme.example.com' });
  expect(result.kind).toBe('retry');
  if (result.kind === 'retry') expect(result.failure.kind).toBe('TRANSPORT');
});

it('classifies an oversized error body from its HTTP status', async () => {
  const provider = client(async () => { throw new BodyTooLargeError(429, { 'retry-after': '2' }); });
  const result = await provider.findContactByEmail().invoke(call('findContactByEmail'), { email: 'jane@acme.example.com' });
  expect(result.kind).toBe('retry');
  if (result.kind === 'retry') { expect(result.failure.kind).toBe('RATE_LIMIT'); expect(result.retryAfterMs).toBe(2000); }
});

it('preserves filter order and escapes reply paragraphs', () => {
  expect(buildConversationSearchQuery({ text: ' refund ', inboxId: 'inb_41w25', statuses: ['open', 'open', 'unassigned'] })).toBe('refund inbox:inb_41w25 is:open is:unassigned');
  expect(buildReplyHTML('Hi <Jane>\n\nRefund & update')).toBe('<p>Hi &lt;Jane&gt;</p><p>Refund &amp; update</p>');
});

it('encodes the full search query as Go QueryEscape with percent-encoded spaces', async () => {
  const urls: string[] = [];
  const provider = client(async request => {
    urls.push(request.url);
    return { status: 200, headers: {}, body: Buffer.from('{"_results":[],"_total":0}') };
  });
  const result = await provider.searchConversations().invoke(call('searchConversations'), {
    text: 'refund', inboxId: 'inb_41w25', statuses: ['open'], recipientEmail: 'jane+tag@acme.example.com',
  });
  expect(result.kind).toBe('branch');
  expect(urls).toEqual(['https://api2.frontapp.com/conversations/search/refund%20inbox%3Ainb_41w25%20is%3Aopen%20recipient%3Ajane%2Btag%40acme.example.com?limit=25']);
});

it('retries transient reply credential failures and ends on reauthorization', async () => {
  const replyCall = { ...call('replyToConversation'), context: new HeartbeatContext(undefined) as unknown as Call['context'] };
  const transient = new FrontClient({}, { resolve: async () => { throw new Error('broker unavailable'); } });
  const first = await transient.replyToConversation().invoke(replyCall, { conversationId: 'cnv_55c8c149', text: 'Hello' });
  expect(first.kind).toBe('retry');
  const terminal = new FrontClient({}, { resolve: async () => { throw new ReauthorizationRequiredError(); } });
  const second = await terminal.replyToConversation().invoke({ ...replyCall, context: new HeartbeatContext(undefined) as unknown as Call['context'] }, { conversationId: 'cnv_55c8c149', text: 'Hello' });
  expect(second.kind).toBe('branch');
  if (second.kind === 'branch') expect(second.branch).toBe('defect');
});
