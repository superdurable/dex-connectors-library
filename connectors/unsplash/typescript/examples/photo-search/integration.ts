// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client as DexClient, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { Client, newConnection, type PhotoPage } from '../../src/index.js';
import { ConnectionName, PhotoSearchFlow } from './flow.js';

/** Verify the checked-in Flow against a real Dex Worker and Client. */
export async function verifyPhotoSearch(serverAddress: string): Promise<void> {
  let requestCount = 0;
  const provider = createServer((request, response) => {
    requestCount++;
    assert.equal(request.headers.authorization, 'Client-ID unsplash-test-key');
    assert.equal(new URL(request.url ?? '/', 'http://localhost').pathname, '/search/photos');
    if (request.url?.includes('reject')) { response.writeHead(401).end('{}'); return; }
    if (requestCount === 1) { response.writeHead(429, { 'Retry-After': '1' }).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ total: 1, total_pages: 1, results: [{ id: 'office-1', urls: { regular: 'https://images.unsplash.com/photo-office-1' }, links: { html: 'https://unsplash.com/photos/office-1' }, user: { name: 'A. Photographer', links: { html: 'https://unsplash.com/@photographer' } } }] }));
  });
  provider.listen(0, '127.0.0.1');
  await once(provider, 'listening');
  const address = provider.address();
  if (!address || typeof address === 'string') throw new Error('Unsplash stand-in port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-unsplash-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const providerClient = new Client({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ accessKey: new SecretString('unsplash-test-key') }) });
  const flow = new PhotoSearchFlow(newConnection(providerClient, { provider: 'unsplash', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new DexClient(registry, cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `unsplash-success-${randomUUID()}`;
    await client.startFlow(flow, successId, { query: 'office', perPage: 5 });
    const success = await client.waitForFlow(successId, 60_000);
    assert.equal(success.status, 'completed', success.errorMessage);
    assert.equal(success.singleOutput<PhotoPage>().photos?.[0]?.id, 'office-1');
    const rejectedId = `unsplash-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { query: 'reject' });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    assert.equal(rejected.status, 'failed');
    assert.match(rejected.errorMessage ?? '', /providerRejected/);
    assert.equal(requestCount, 3);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => provider.close(() => resolve()));
  }
}
const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
if (!serverAddress) throw new Error('DEX_FLOW_SERVICE_ADDRESS is required for the integration check');
await verifyPhotoSearch(serverAddress);
