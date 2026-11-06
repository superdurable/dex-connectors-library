// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { ReamazeClient } from '../../src/client.js';
import { newConnection, type SearchConversationsOutput } from '../../src/index.js';
import { ConnectionName, SearchConversationsFlow } from '../../examples/search-conversations/flow/workflow.js';

it('searches through a real Dex Worker and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.method !== 'GET' || !request.url?.startsWith('/api/v1/conversations?')) { response.writeHead(404).end(); return; }
    if (request.headers.authorization !== `Basic ${Buffer.from('agent@acme.example.com:testToken123').toString('base64')}`) { response.writeHead(401).end(); return; }
    const query = new URL(request.url, 'http://localhost').searchParams;
    if (query.get('for') === 'reject@example.com') { response.writeHead(403).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ page_size: 30, page_count: 1, total_count: 1, conversations: [{ slug: 'double-charge', status: 0, created_at: '2026-01-26T14:02:00Z', tag_list: ['billing'], category: { slug: 'support', channel: 1 } }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Re:amaze fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-reamaze-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new ReamazeClient({ brand: 'acme' }, { resolve: async () => ({ email: 'agent@acme.example.com', apiToken: new SecretString('testToken123') }) }, { apiBaseURL: `http://127.0.0.1:${address.port}/api/v1` });
  const flow = new SearchConversationsFlow(newConnection(provider, { provider: 'reamaze', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `reamaze-search-${randomUUID()}`;
    await client.startFlow(flow, successId, { requesterEmail: 'jane@example.com' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<SearchConversationsOutput>().conversations?.[0]?.id).toBe('double-charge');
    const rejectedId = `reamaze-search-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { requesterEmail: 'reject@example.com' });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
