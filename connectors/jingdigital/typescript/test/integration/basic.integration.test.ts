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
import { JINGdigitalClient } from '../../src/client.js';
import { newConnection, type Credentials } from '../../src/index.js';
import { ConnectionName, JINGdigitalInspectionFlow } from '../../examples/basic/flow.js';

it('retries a provider 503 and persists the result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    if (request.url === '/v1/oauth2/token') { response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ code: 0, data: { access_token: 'temporary-token' } })); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ code: 0, data: { items: [{ id: 166007, name: 'Example Tag' }], _meta: { totalCount: 1, currentPage: 2, perPage: 10 } } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('JINGdigital fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-jingdigital-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { appId: new SecretString('app-test'), appSecret: new SecretString('secret-test') };
  const provider = new JINGdigitalClient({}, { resolve: async () => credentials }, { send: async request => { const reply = await fetch(`http://127.0.0.1:${address.port}${new URL(request.url).pathname}`, { method: request.method, headers: request.headers, body: request.body ? Buffer.from(request.body) : undefined }); return { status: reply.status, headers: Object.fromEntries(reply.headers), body: new Uint8Array(await reply.arrayBuffer()) }; } });
  const connection = newConnection(provider, { provider: 'jingdigital', name: ConnectionName });
  const flow = new JINGdigitalInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `jingdigital-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { page: 2, pageSize: 10 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({ tags: [{ id: 166007, name: 'Example Tag', level: 0 }], total: 1, nextPage: 0 });
    expect(attempts).toBe(3);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
