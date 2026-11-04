// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { TypeformClient } from '../src/client.js';
import { decodeCredentials } from '../src/generated/models.js';

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
