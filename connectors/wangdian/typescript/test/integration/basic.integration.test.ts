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
import { WangdianClient } from '../../src/client.js';
import { newConnection, type Credentials } from '../../src/index.js';
import { ConnectionName, WangdianInspectionFlow } from '../../examples/basic/flow.js';

it('retries a provider 503 and persists the result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((_request, response) => {
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ code: 0, shoplist: [{ shop_id: '332', shop_no: 'SHOP-332', shop_name: 'Example Shop', platform_id: 127, auth_state: 1 }], total_count: 21 }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Wangdian fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-wangdian-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { sid: new SecretString('test2'), appKey: new SecretString('test2-xx'), appSecret: new SecretString('12345') };
  const provider = new WangdianClient({}, { resolve: async () => credentials }, { send: async request => {
    const target = new URL(request.url);
    const reply = await fetch(`http://127.0.0.1:${address.port}${target.pathname}${target.search}`, { method: request.method, headers: request.headers, body: request.body ? Buffer.from(request.body) : undefined });
    return { status: reply.status, headers: Object.fromEntries(reply.headers), body: new Uint8Array(await reply.arrayBuffer()) };
  } });
  const connection = newConnection(provider, { provider: 'wangdian', name: ConnectionName });
  const flow = new WangdianInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `wangdian-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { page: 2, pageSize: 10 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({ shops: [{ id: '332', number: 'SHOP-332', name: 'Example Shop', platformID: 127, authorizationState: 1 }], total: 21, nextPage: 0 });
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
