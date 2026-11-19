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
import { OppoClient } from '../../src/client.js';
import { newConnection } from '../../src/index.js';
import { ConnectionName, OppoInspectionFlow } from '../../examples/basic/flow.js';

it('retries one 503 and persists the provider result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const bodies = [{"code": 0, "data": {"auth_token": "secret-token"}}, {"code": 0, "data": {"total_count": 100000, "push_count": 10000, "remain_count": 90000, "secret": "do-not-return"}}];
  const server = createServer((request, response) => {
    requests++;
    if (requests === 1) { response.writeHead(503).end(); return; }
    const selected = request.url?.includes("fetch_push_permit") ? bodies[1] : bodies[0];
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify(selected));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Oppo fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-oppo-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new OppoClient({}, { resolve: async () => ({ appKey: new SecretString('test-app-key'), masterSecret: new SecretString('test-master-secret') }) }, {
    now: () => new Date(1731510000000),
    send: async request => {
      const reply = await fetch(`http://127.0.0.1:${address.port}${new URL(request.url).pathname}`, { method: request.method, headers: request.headers, body: request.body ? Buffer.from(request.body) : undefined });
      return { status: reply.status, headers: Object.fromEntries(reply.headers), body: new Uint8Array(await reply.arrayBuffer()) };
    },
  });
  const connection = newConnection(provider, { provider: 'oppo', name: ConnectionName });
  const flow = new OppoInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `oppo-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({"pushCount": 10000, "remainCount": 90000, "totalCount": 100000});
    expect(requests).toBe(3);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
