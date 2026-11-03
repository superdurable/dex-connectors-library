// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { CloudflareClient } from '../src/client.js';

const call = {
  id: 'test-call', connection: { provider: 'cloudflare', name: 'cloudflare' },
  context: { flowId: 'test-flow', cancellationSignal: new AbortController().signal },
} as Call;

function client(send: (request: ProviderRequest) => Promise<{ status: number; headers: Record<string, string>; body: Uint8Array }>) {
  return new CloudflareClient({ baseUrl: 'http://127.0.0.1:21201' }, {
    resolve: async () => ({ apiToken: new SecretString('test-apitoken') }),
  }, { send });
}

it('rejects malformed purge inputs before a provider request', async () => {
  let requests = 0;
  const operation = client(async () => { requests++; throw new Error('unexpected request'); }).purgeCacheByURLs();
  const missingZone = await operation.invoke(call, { zoneId: '', urls: ['https://meridian.example/'] });
  const invalidURL = await operation.invoke(call, { zoneId: 'zone-meridian', urls: ['ftp://meridian.example/'] });
  expect(missingZone.kind === 'branch' && missingZone.branch).toBe('defect');
  expect(invalidURL.kind === 'branch' && invalidURL.branch).toBe('defect');
  expect(requests).toBe(0);
});

it('matches Go URL validation for purge inputs', async () => {
  const acceptedURLs = ['https://meridian.example/a b', 'http://meridian.example/?q=a b', 'https://meridian.example:/', 'https://meridian.example:99999/'];
  const rejectedURLs = ['https:///x', 'https:example.com', 'https://@meridian.example/x', 'https://meridian.example/%zz', 'https://meridian.example:abc/x', 'https://meri dian.example/x'];
  const requests: string[] = [];
  const operation = client(async request => {
    requests.push(Buffer.from(request.body ?? []).toString('utf8'));
    return { status: 200, headers: {}, body: Buffer.from('{"success":true,"result":{"id":"purge-id"}}') };
  }).purgeCacheByURLs();
  for (const url of acceptedURLs) {
    const result = await operation.invoke(call, { zoneId: 'zone-meridian', urls: [url] });
    expect(result.kind === 'branch' && result.branch, url).toBe('purged');
  }
  for (const url of rejectedURLs) {
    const result = await operation.invoke(call, { zoneId: 'zone-meridian', urls: [url] });
    expect(result.kind === 'branch' && result.branch, url).toBe('defect');
  }
  expect(requests.map(request => JSON.parse(request) as unknown)).toEqual(acceptedURLs.map(url => ({ files: [url] })));
});

it('keeps provider rejection details out of failures', async () => {
  const operation = client(async () => ({ status: 200, headers: {}, body: Buffer.from('{"success":false,"errors":[{"message":"SENTINEL"}]}') })).purgeCacheByURLs();
  const result = await operation.invoke(call, { zoneId: 'zone-meridian', urls: ['https://meridian.example/'] });
  expect(result.kind === 'branch' && result.branch).toBe('providerRejected');
  expect(JSON.stringify(result)).not.toContain('SENTINEL');
});

it('classifies bounded body and transport failures', async () => {
  const oversized = await client(async () => { throw new BodyTooLargeError(200); }).listZones().invoke(call, {});
  expect(oversized.kind === 'branch' && oversized.branch).toBe('invalidResponse');
  const transport = await client(async () => { throw new ProviderTransportError(true); }).listZones().invoke(call, {});
  expect(transport.kind).toBe('retry');
});

it('classifies oversized error bodies by status before reading content', async () => {
  const rateLimit = await client(async () => { throw new BodyTooLargeError(429, { 'retry-after': '2' }); }).listZones().invoke(call, {});
  expect(rateLimit.kind).toBe('retry');
  if (rateLimit.kind === 'retry') expect(rateLimit.retryAfterMs).toBe(2_000);
  const notFound = await client(async () => { throw new BodyTooLargeError(404, {}); }).purgeCacheByURLs().invoke(call, {
    zoneId: 'zone-meridian', urls: ['https://meridian.example/'],
  });
  expect(notFound.kind === 'branch' && notFound.branch).toBe('notFound');
});

it('decodes sparse success fields using Go zero values', async () => {
  const zones = await client(async () => ({ status: 200, headers: {}, body: Buffer.from('{"success":true,"result":[{},null]}') })).listZones().invoke(call, {});
  expect(zones.kind === 'branch' && zones.value).toEqual({ success: true, result: [
    { id: '', name: '', status: '' }, { id: '', name: '', status: '' },
  ] });
  const purge = await client(async () => ({ status: 200, headers: {}, body: Buffer.from('{"success":true}') })).purgeCacheByURLs().invoke(call, {
    zoneId: 'zone-meridian', urls: ['https://meridian.example/'],
  });
  expect(purge.kind === 'branch' && purge.value).toEqual({ success: true, result: { id: '' } });
});
