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
import { FreshdeskClient } from '../../src/client.js';
import { newConnection, type SearchTicketsOutput } from '../../src/index.js';
import { ConnectionName, SearchTicketsFlow } from '../../examples/search-tickets/flow/workflow.js';

it('searches through real Dex and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.method !== 'GET' || !request.url?.startsWith('/api/v2/search/tickets?')) { response.writeHead(404).end(); return; }
    if (request.headers.authorization !== `Basic ${Buffer.from('freshdeskTestKey0123456789:X').toString('base64')}`) { response.writeHead(401).end(); return; }
    if (request.url.includes('status%3A5')) { response.writeHead(403, { 'Content-Type': 'application/json' }).end('{"code":"forbidden"}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ total: 1, results: [{ id: 1201, subject: 'Billing issue', status: 2, priority: 3, requester_id: 6001, created_at: '2026-01-29T10:00:00Z', updated_at: '2026-01-29T10:00:00Z' }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Freshdesk fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-freshdesk-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new FreshdeskClient({ domain: 'acme' }, { resolve: async () => ({ apiKey: new SecretString('freshdeskTestKey0123456789') }) }, { apiBaseURL: `http://127.0.0.1:${address.port}/api/v2` });
  const flow = new SearchTicketsFlow(newConnection(provider, { provider: 'freshdesk', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `freshdesk-search-${randomUUID()}`;
    await client.startFlow(flow, successId, { statuses: [2], page: 1 });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<SearchTicketsOutput>().tickets?.[0]?.id).toBe(1201);
    const rejectionId = `freshdesk-search-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { statuses: [5], page: 1 });
    const rejected = await client.waitForFlow(rejectionId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
