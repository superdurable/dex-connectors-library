// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { HeapClient } from '../../src/client.js';
import { newConnection, type UserPropertiesResult } from '../../src/index.js';
import { ConnectionName, UserPropertiesFlow } from '../../examples/user-properties/flow.js';

it('retries a provider 503 and persists acceptance through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.method !== 'POST' || request.url !== '/api/add_user_properties') { response.writeHead(404).end(); return; }
    const chunks: Uint8Array[] = [];
    request.on('data', chunk => chunks.push(chunk));
    request.on('end', () => {
      expect(JSON.parse(Buffer.concat(chunks).toString('utf8'))).toEqual({
        app_id: '12345', identity: 'member-1', properties: { Plan: 'Pro' },
      });
      attempts++;
      response.writeHead(attempts === 1 ? 503 : 200).end();
    });
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Heap fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-heap-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new HeapClient({ appId: '12345', baseUrl: `http://127.0.0.1:${address.port}` });
  const connection = newConnection(provider, { provider: 'heap', name: ConnectionName });
  const flow = new UserPropertiesFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `heap-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { identity: 'member-1', properties: { Plan: 'Pro' } });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<UserPropertiesResult>().accepted).toBe(true);
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
