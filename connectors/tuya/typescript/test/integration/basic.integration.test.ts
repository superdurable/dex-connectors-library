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
import { TuyaClient } from '../../src/client.js';
import { newConnection, type Credentials } from '../../src/index.js';
import { ConnectionName, TuyaInspectionFlow } from '../../examples/basic/flow.js';

it('retries a provider 503 and persists the result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    if (request.url?.startsWith('/v1.0/token')) { response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ success: true, result: { access_token: 'integration-token', expire_time: 7200 } })); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ success: true, result: { id: 'device123', name: 'Scale', online: true, category: 'qt', product_id: 'product42', create_time: 1560827137, update_time: 1589764585, local_key: 'must-not-persist' } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Tuya fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-tuya-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { clientId: new SecretString('integration-client'), clientSecret: new SecretString('integration-secret') };
  const provider = new TuyaClient({}, { resolve: async () => credentials }, { send: async request => { const reply = await fetch(`http://127.0.0.1:${address.port}${new URL(request.url).pathname}${new URL(request.url).search}`, { method: request.method, headers: request.headers, body: request.body ? Buffer.from(request.body) : undefined }); return { status: reply.status, headers: Object.fromEntries(reply.headers), body: new Uint8Array(await reply.arrayBuffer()) }; } });
  const connection = newConnection(provider, { provider: 'tuya', name: ConnectionName });
  const flow = new TuyaInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `tuya-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { deviceId: 'device123' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({ deviceId: 'device123', name: 'Scale', online: true, category: 'qt', productId: 'product42', createdAtUnix: 1560827137, updatedAtUnix: 1589764585 });
    expect(attempts).toBe(3);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
