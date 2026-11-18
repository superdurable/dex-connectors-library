// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
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

const reply = { conversationId: 'cnv_55c8c149', text: 'Hello' };
const tagging = { conversationId: 'cnv_55c8c149', addTagIds: ['tag_100'] };
const conversationBody = Buffer.from('{"id":"cnv_55c8c149","status":"archived","tags":[]}');

function heartbeatCall(operationId: string, context = new HeartbeatContext(undefined)): Call {
  return { ...call(operationId), context: context as unknown as Call['context'] };
}

it('never resends a reply whose 2xx answer has no usable provider ID', async () => {
  for (const failure of [
    async () => ({ status: 202, headers: {}, body: Buffer.from('{"status":"accepted"}') }),
    async () => ({ status: 202, headers: {}, body: Buffer.from('{invalid') }),
    async () => { throw new ProviderTransportError(false, 202, {}); },
    async () => { throw new ProviderStalledError(202, {}); },
    async () => { throw new BodyTooLargeError(202, {}); },
  ]) {
    let sends = 0;
    const provider = client(async () => { sends++; return failure(); });
    const result = await provider.replyToConversation().invoke(heartbeatCall('replyToConversation'), reply);
    expect(result.kind).toBe('uncertain');
    expect(sends).toBe(1);
  }
});

it('passes the fence write-window signal to the reply request', async () => {
  const replyCall = heartbeatCall('replyToConversation');
  let signal: AbortSignal | undefined;
  const provider = client(async request => { signal = request.signal; return { status: 202, headers: {}, body: Buffer.from('{"message_uid":"abc123"}') }; });
  await provider.replyToConversation().invoke(replyCall, reply);
  expect(signal).toBeDefined();
  expect(signal).not.toBe(replyCall.context.cancellationSignal);
});

it('treats a reply refusal with an unreadable body as an unknown outcome', async () => {
  const provider = client(async () => { throw new BodyTooLargeError(400, {}); });
  const result = await provider.replyToConversation().invoke(heartbeatCall('replyToConversation'), reply);
  expect(result.kind).toBe('retry');
});

it('never selects defect for a reply credential failure after a possible send', async () => {
  for (const [error, kind] of [[new ReauthorizationRequiredError(), 'uncertain'], [new TypeError('broker unavailable'), 'retry']] as const) {
    const context = new HeartbeatContext(undefined);
    const first = await client(async () => ({ status: 502, headers: {}, body: Buffer.from('{}') })).replyToConversation().invoke(heartbeatCall('replyToConversation', context), reply);
    expect(first.kind).toBe('retry');
    const marker = context.marker;
    context.attempt = 2;
    const failing = new FrontClient({}, { resolve: async () => { throw error; } });
    const second = await failing.replyToConversation().invoke(heartbeatCall('replyToConversation', context), reply);
    expect(second.kind).toBe(kind);
    expect(context.marker).toBe(marker);
  }
});

it('retries an update write whose refusal body cannot be read', async () => {
  const provider = client(async request => {
    if (request.method === 'GET') return { status: 200, headers: {}, body: conversationBody };
    throw new BodyTooLargeError(400, {});
  });
  const result = await provider.updateConversation().invoke(call('updateConversation'), tagging);
  expect(result.kind).toBe('retry');
});

it('selects defect for an update credential failure only on a terminal first attempt', async () => {
  for (const [error, attempt, kind] of [
    [new TypeError('broker unavailable'), 1, 'retry'],
    [new ReauthorizationRequiredError(), 1, 'branch'],
    [new ReauthorizationRequiredError(), 2, 'retry'],
  ] as const) {
    const context = new HeartbeatContext(undefined);
    context.attempt = attempt;
    const failing = new FrontClient({}, { resolve: async () => { throw error; } }, { send: async () => { throw new Error('no request may be sent'); } });
    const result = await failing.updateConversation().invoke(heartbeatCall('updateConversation', context), tagging);
    expect(result.kind).toBe(kind);
    if (result.kind === 'branch') expect(result.branch).toBe('defect');
  }
});

it('retries transient reply credential failures and ends on reauthorization', async () => {
  const replyCall = { ...call('replyToConversation'), context: new HeartbeatContext(undefined) as unknown as Call['context'] };
  const transient = new FrontClient({}, { resolve: async () => { throw new TypeError('project credential decoding failed'); } });
  const first = await transient.replyToConversation().invoke(replyCall, { conversationId: 'cnv_55c8c149', text: 'Hello' });
  expect(first.kind).toBe('retry');
  const terminal = new FrontClient({}, { resolve: async () => { throw new ReauthorizationRequiredError(); } });
  const second = await terminal.replyToConversation().invoke({ ...replyCall, context: new HeartbeatContext(undefined) as unknown as Call['context'] }, { conversationId: 'cnv_55c8c149', text: 'Hello' });
  expect(second.kind).toBe('branch');
  if (second.kind === 'branch') expect(second.branch).toBe('defect');
});
