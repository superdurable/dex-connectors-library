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
import { KitClient } from '../../src/client.js';
import { newConnection, type SubscribersPage } from '../../src/index.js';
import { ConnectionName, SubscriberInspectionFlow } from '../../examples/subscriber-inspection/flow.js';

it('retries a provider 503 and persists the subscriber page through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers['x-kit-api-key'] !== 'synthetic-api-key') { response.writeHead(401).end(); return; }
    if (request.url !== '/v4/subscribers?per_page=20&slim=true') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ subscribers: [{ id: 204, email_address: 'ada@example.com', first_name: 'Ada', state: 'active' }], pagination: { has_next_page: false, end_cursor: '', per_page: 20 } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Kit fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-kit-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new KitClient({ baseUrl: `http://127.0.0.1:${address.port}/v4` }, {
    resolve: async () => ({ apiKey: new SecretString('synthetic-api-key') }),
  });
  const connection = newConnection(provider, { provider: 'kit', name: ConnectionName });
  const flow = new SubscriberInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `kit-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { perPage: 20 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<SubscribersPage>().subscribers?.[0]?.id).toBe(204);
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
