// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call, type QueryAttempt } from '@superdurable/connectors-sdk';
import { Client } from '../src/client.js';

const connection = { provider: 'unsplash', name: 'unsplash' };
function call(operationId: string): Call {
  return { context: { flowId: 'unsplash-test', cancellationSignal: new AbortController().signal } as Call['context'], id: 'test-call', connection, operation: { connectorId: 'unsplash', operationId }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
}
function branch<Output>(attempt: QueryAttempt<Output>): string { return attempt.kind === 'branch' ? attempt.branch : 'retry'; }
const credentials = { resolve: async () => ({ accessKey: new SecretString('test-key') }) };

it('escapes a photo ID like Go url.PathEscape', async () => {
  let requestedURL = '';
  const client = new Client({}, credentials, { send: async request => { requestedURL = request.url; return { status: 404, headers: {}, body: new Uint8Array() }; } });
  const result = await client.getPhoto().invoke(call('getPhoto'), { id: "a b!$'()*+,;=:@~" });
  expect(branch(result)).toBe('notFound');
  expect(requestedURL).toContain('/photos/a%20b%21$%27%28%29%2A+%2C%3B=:@~');
});

it('escapes search parameters like Go url.Values.Encode', async () => {
  let requestedURL = '';
  const client = new Client({}, credentials, { send: async request => { requestedURL = request.url; return { status: 200, headers: {}, body: Buffer.from('{"total":0,"total_pages":0,"results":[]}') }; } });
  const result = await client.searchPhotos().invoke(call('searchPhotos'), { query: "a*b~c !'()", page: 2, perPage: 10 });
  expect(branch(result)).toBe('searched');
  expect(requestedURL).toBe('https://api.unsplash.com/search/photos?page=2&per_page=10&query=a%2Ab~c+%21%27%28%29');
});

it.each(['https:///x', 'https:example.com/a'])('rejects a photo URL without a Go URL host: %s', async htmlURL => {
  const photo = { id: 'office-1', links: { html: htmlURL }, urls: { regular: 'https://images.unsplash.com/office-1' }, user: { name: 'Photographer', links: { html: 'https://unsplash.com/@photographer' } } };
  const client = new Client({}, credentials, { send: async () => ({ status: 200, headers: {}, body: Buffer.from(JSON.stringify(photo)) }) });
  const result = await client.getPhoto().invoke(call('getPhoto'), { id: 'office-1' });
  expect(branch(result)).toBe('invalidResponse');
  expect(result.failure?.kind).toBe('PROTOCOL');
});

it('rejects wrong-typed provider fields as an invalid response', async () => {
  const client = new Client({}, credentials, { send: async () => ({ status: 200, headers: {}, body: Buffer.from('{"total":1,"total_pages":1,"results":[{"id":42}]}') }) });
  const result = await client.searchPhotos().invoke(call('searchPhotos'), { query: 'office' });
  expect(branch(result)).toBe('invalidResponse');
  expect(result.failure?.kind).toBe('PROTOCOL');
});

it('keeps credential failures terminal and local input out of the provider', async () => {
  let requestCount = 0;
  const client = new Client({}, { resolve: async () => { throw new Error('private failure'); } }, { send: async () => { requestCount++; return { status: 200, headers: {}, body: new Uint8Array() }; } });
  expect(branch(await client.getPhoto().invoke(call('getPhoto'), { id: 'office-1' }))).toBe('defect');
  expect(branch(await client.searchPhotos().invoke(call('searchPhotos'), { query: '' }))).toBe('defect');
  expect(requestCount).toBe(0);
});

it('passes the configured timeout to the provider transport', async () => {
  let timeoutMs = 0;
  const client = new Client({ timeoutMs: 2400 }, credentials, { send: async request => { timeoutMs = request.timeoutMs ?? 0; return { status: 404, headers: {}, body: new Uint8Array() }; } });
  await client.getPhoto().invoke(call('getPhoto'), { id: 'office-1' });
  expect(timeoutMs).toBe(2400);
});
