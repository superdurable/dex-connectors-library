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
import { ConnectionName, TaskStatusFlow } from '../../examples/task-status/flow.js';
import { ShengshuClient } from '../../src/client.js';
import { newConnection, type GetTaskStatusOutput } from '../../src/index.js';

it('retries Shengshu 503 through a real Dex Worker and persists the task status', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'GET' || request.url !== '/ent/v2/tasks/task-123/creations' || request.headers.authorization !== 'Token test-key' || request.headers.accept !== 'application/json') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ id: 'task-123', state: 'success', err_code: '', credits: 4, creations: [{ id: 'creation-456' }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Shengshu fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-shengshu-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new ShengshuClient({}, { resolve: async () => ({ apiKey: new SecretString('test-key') }) }, {
    send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}/ent/v2/tasks/task-123/creations`, addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new TaskStatusFlow(newConnection(provider, { provider: 'shengshu', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `shengshu-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { taskId: 'task-123' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<GetTaskStatusOutput>()).toEqual({ taskId: 'task-123', state: 'success', errorCode: '', credits: 4, creationIds: ['creation-456'] });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
