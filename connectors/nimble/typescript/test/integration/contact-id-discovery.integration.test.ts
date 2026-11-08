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
import { NimbleClient } from '../../src/client.js';
import { newConnection, type ContactIDsPage } from '../../src/index.js';
import { ConnectionName, ContactIDDiscoveryFlow } from '../../examples/contact-id-discovery/flow.js';

it('retries a provider 503 and persists contact IDs through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer synthetic-api-key') { response.writeHead(401).end(); return; }
    if (request.url !== '/api/v1/contacts/ids?page=1&per_page=20') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ meta: { page: 1, pages: 1, per_page: 20, total: 1 }, resources: ['5049fb849b85f669e40000dc'] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Nimble fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-nimble-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new NimbleClient({ baseUrl: `http://127.0.0.1:${address.port}/api/v1` }, {
    resolve: async () => ({ apiKey: new SecretString('synthetic-api-key') }),
  });
  const connection = newConnection(provider, { provider: 'nimble', name: ConnectionName });
  const flow = new ContactIDDiscoveryFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `nimble-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { page: 1, perPage: 20 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<ContactIDsPage>().ids?.[0]).toBe('5049fb849b85f669e40000dc');
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
