// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { SecretString } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { ConnectionName, RoomListFlow } from '../../examples/rooms/flow.js';
import { LikelicClient } from '../../src/client.js';
import { newConnection, type ListRoomsOutput } from '../../src/index.js';

it('retries Likelic 503 through a real Dex Worker and persists room identifiers', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'GET' || request.url !== '/api/v1/rooms?page=1&limit=20' || request.headers['x-customer-code'] !== 'demo_company' || request.headers['x-api-key'] !== 'sk-0123456789abcdef0123456789abcdef0123456789abcdef') { response.writeHead(400).end(); return; }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ success: true, data: [{ channel: 'wework', instance_id: 3, room_id: 'room-123', push_state: 'ready', room_name: 'secret' }], meta: { page: 1, limit: 20, total: null } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Likelic fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-likelic-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new LikelicClient({}, { resolve: async () => ({ apiKey: new SecretString('sk-0123456789abcdef0123456789abcdef0123456789abcdef'), customerCode: 'demo_company' }) }, {
    send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}${new URL(request.url).pathname}${new URL(request.url).search}`, addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new RoomListFlow(newConnection(provider, { provider: 'likelic', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `likelic-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { page: 1 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<ListRoomsOutput>()).toEqual({ page: 1, total: 0, rooms: [{ channel: 'wework', instanceID: 3, roomID: 'room-123', pushState: 'ready' }] });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
