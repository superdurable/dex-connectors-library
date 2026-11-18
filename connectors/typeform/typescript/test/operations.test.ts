// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call, type QueryAttempt } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { BodyTooLargeError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { TypeformClient } from '../src/client.js';
import { CredentialMaterialError, decodeCredentials } from '../src/generated/models.js';

/** Reproduce the SDK project provider, which replaces the decoder error with this TypeError. */
function sdkDecodingFailure(): TypeError {
  try { decodeCredentials({ access_token: 7 }); } catch { return new TypeError('project credential decoding failed'); }
  throw new Error('malformed material decoded');
}

function branchOf<Value>(attempt: QueryAttempt<Value>): string { return attempt.kind === 'branch' ? attempt.branch : 'retry'; }

const call = { id: 'test-call', context: { cancellationSignal: new AbortController().signal } } as Call;

it('decodes a token without an optional webhook secret for every read operation', async () => {
  const credentials = decodeCredentials({ access_token: 'test-token' });
  expect(credentials.webhookSecret).toBeUndefined();
  const requests: ProviderRequest[] = [];
  const client = new TypeformClient({}, { resolve: async () => credentials }, { send: async request => {
    requests.push(request);
    const pathname = new URL(request.url).pathname;
    const body = pathname.endsWith('/responses') ? { total_items: 0, page_count: 0, items: [] } : pathname === '/forms' ? { total_items: 0, page_count: 0, items: [] } : { id: 'u6nXL7', title: 'A form' };
    return { status: 200, headers: {}, body: Buffer.from(JSON.stringify(body)) };
  } });
  expect(branchOf(await client.listForms().invoke(call, {}))).toBe('listed');
  expect(branchOf(await client.getForm().invoke(call, { formId: 'u6nXL7' }))).toBe('found');
  expect(branchOf(await client.listResponses().invoke(call, { formId: 'u6nXL7' }))).toBe('listed');
  expect(requests).toHaveLength(3);
});

it('retries unavailable credentials but selects defect for an unusable token', async () => {
  const unreadable = new TypeformClient({}, { resolve: async () => { throw new Error('temporarily unavailable'); } });
  expect((await unreadable.listForms().invoke(call, {})).failure?.kind).toBe('AVAILABILITY');
  const invalid = new TypeformClient({}, { resolve: async () => ({ accessToken: new SecretString('bad\ntoken') }) });
  expect(branchOf(await invalid.listForms().invoke(call, {}))).toBe('defect');
});

it.each([
  ['reauthorization', () => new ReauthorizationRequiredError(), true],
  ['missing connection object', () => new ObjectNotFoundError(), true],
  ['unselected connection', () => new ConnectionNotSelectedError(), true],
  ['broker refusal', () => new CredentialBrokerRejectedError(), true],
  ['malformed stored credential', () => new CredentialMaterialError('credential access_token is required'), true],
  ['SDK-wrapped malformed stored credential', () => sdkDecodingFailure(), true],
  ['broker outage', () => new CredentialBrokerUnavailableError(), false],
  ['unconfigured static connection', () => new Error('connection is not configured'), false],
] as const)('classifies a %s resolve failure the same way for reads and the first upsert', async (_name, error, isTerminal) => {
  const client = new TypeformClient({}, { resolve: async () => { throw error(); } }, { send: async () => { throw new Error('no request may be sent'); } });
  const expected = isTerminal ? 'defect' : 'retry';
  expect(branchOf(await client.listForms().invoke(call, {}))).toBe(expected);
  const upsert = { formId: 'u6nXL7', tag: 'hook', url: 'https://hooks.example.com/t' };
  const first = await client.upsertWebhook().invoke({ ...call, context: { ...call.context, attempt: 1 } } as Call, upsert);
  expect(first.kind === 'branch' ? first.branch : first.kind).toBe(expected);
  // A later attempt may follow an applied PUT, so it retries instead of selecting defect.
  const later = await client.upsertWebhook().invoke({ ...call, context: { ...call.context, attempt: 2 } } as Call, upsert);
  expect(later.kind).toBe('retry');
});

it('rejects invalid provider types and calendar dates', async () => {
  const client = new TypeformClient({}, { resolve: async () => ({ accessToken: new SecretString('test-token') }) }, { send: async () => ({ status: 200, headers: {}, body: Buffer.from('{"total_items":1,"page_count":1,"items":[{"id":"u6nXL7","title":7}]}') }) });
  expect(branchOf(await client.listForms().invoke(call, {}))).toBe('invalidResponse');
  const invalidDate = new TypeformClient({}, { resolve: async () => ({ accessToken: new SecretString('test-token') }) }, { send: async () => ({ status: 200, headers: {}, body: Buffer.from('{"total_items":1,"page_count":1,"items":[{"id":"u6nXL7","title":"A","created_at":"2026-02-30T12:00:00Z"}]}') }) });
  expect(branchOf(await invalidDate.listForms().invoke(call, {}))).toBe('invalidResponse');
});

it('encodes a form search with Go query escaping and sorted keys', async () => {
  let requestURL = '';
  const client = new TypeformClient({}, { resolve: async () => ({ accessToken: new SecretString('test-token') }) }, { send: async request => {
    requestURL = request.url;
    return { status: 200, headers: {}, body: Buffer.from('{"total_items":0,"page_count":0,"items":[]}') };
  } });
  expect(branchOf(await client.listForms().invoke(call, { search: 'lead form~*', workspaceId: 'workspace_1' }))).toBe('listed');
  expect(new URL(requestURL).search).toBe('?page=1&search=lead+form%7E%2A&workspace_id=workspace_1');
});

it.each([
  ['an unlisted refusal status', 405, '{"code":"METHOD_NOT_ALLOWED"}'],
  ['a conflict', 409, '{"code":"CONFLICT"}'],
  ['a refusal carrying a webhook', 400, '{"id":"webhook_1","code":"VALIDATION_ERROR"}'],
  ['a malformed 2xx', 200, '{"id":'],
  ['a 2xx for another tag', 200, '{"id":"w1","form_id":"u6nXL7","tag":"other","url":"https://x.test","enabled":true}'],
] as const)('repeats the idempotent webhook PUT after %s', async (_name, status, body) => {
  const client = new TypeformClient({}, { resolve: async () => ({ accessToken: new SecretString('test-token'), webhookSecret: new SecretString('secret') }) },
    { send: async () => ({ status, headers: {}, body: Buffer.from(body) }) });
  const attempt = await client.upsertWebhook().invoke(call, { formId: 'u6nXL7', tag: 'hook', url: 'https://hooks.example.com/t' });
  expect(attempt.kind).toBe('retry');
  expect(attempt.failure?.kind).toBe('PROTOCOL');
});

it('selects providerRejected for a 402 plan refusal without repeating the PUT', async () => {
  let requests = 0;
  const client = new TypeformClient({}, { resolve: async () => ({ accessToken: new SecretString('test-token'), webhookSecret: new SecretString('secret') }) },
    { send: async () => { requests++; return { status: 402, headers: {}, body: Buffer.from('{"code":"PAYMENT_REQUIRED"}') }; } });
  const attempt = await client.upsertWebhook().invoke(call, { formId: 'u6nXL7', tag: 'hook', url: 'https://hooks.example.com/t' });
  expect(attempt).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'PROVIDER_REJECTION' } });
  expect(requests).toBe(1);
});

it('repeats the idempotent webhook PUT when a refusal body is oversized', async () => {
  const client = new TypeformClient({}, { resolve: async () => ({ accessToken: new SecretString('test-token'), webhookSecret: new SecretString('secret') }) },
    { send: async () => { throw new BodyTooLargeError(403, {}); } });
  const attempt = await client.upsertWebhook().invoke(call, { formId: 'u6nXL7', tag: 'hook', url: 'https://hooks.example.com/t' });
  expect(attempt.kind).toBe('retry');
});
