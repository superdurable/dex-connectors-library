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
import { HiverClient } from '../../src/client.js';
import { newConnection, type ListInboxesOutput } from '../../src/index.js';
import { ConnectionName, ListHiverInboxesFlow } from '../../examples/list-inboxes/flow.js';

it('lists inboxes through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.method !== 'GET' || request.url?.split('?')[0] !== '/v1/inboxes') { response.writeHead(404).end(); return; }
    if (request.headers.authorization !== 'Bearer hiverTestKey0123456789') { response.writeHead(401).end(); return; }
    if (new URL(request.url, 'http://localhost').searchParams.get('next_page') === 'reject') { response.writeHead(401).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ data: { results: [{ id: 105902, display_name: 'Support', email: 'support@acme.example.com', channel_type: 'email', is_authorised: true }], pagination: { next_page: null } } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Hiver fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-hiver-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new HiverClient({ requestIntervalMilliseconds: 1 }, { resolve: async () => ({ apiKey: new SecretString('hiverTestKey0123456789') }) }, { apiBaseURL: `http://127.0.0.1:${address.port}/v1` });
  const flow = new ListHiverInboxesFlow(newConnection(provider, { provider: 'hiver', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `hiver-list-${randomUUID()}`;
    await client.startFlow(flow, successId, { pageSize: 50 });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<ListInboxesOutput>().inboxes?.[0]?.id).toBe('105902');
    const rejectedId = `hiver-list-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { pageToken: 'reject' });
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
