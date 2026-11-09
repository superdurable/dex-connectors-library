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
import { WorkdayClient } from '../../src/client.js';
import { newConnection, type WorkerSummary } from '../../src/index.js';
import { ConnectionName, WorkerFlow } from '../../examples/worker-inspection/flow.js';

it('retries a provider 503 and persists a worker through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer synthetic-workday-token') { response.writeHead(401).end(); return; }
    const requestURL = new URL(request.url ?? '', 'http://127.0.0.1');
    if (requestURL.pathname !== '/api/common/v1/demo/workers/0123456789abcdef0123456789abcdef') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ id: '0123456789abcdef0123456789abcdef', descriptor: 'Ada Lovelace' }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Worker fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-workday-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new WorkdayClient({
    baseUrl: `http://127.0.0.1:${address.port}/api/common/v1/demo`,
    authorizationEndpoint: `http://127.0.0.1:${address.port}/authorize`,
    tokenEndpoint: `http://127.0.0.1:${address.port}/token`,
    scopes: ['Staffing'],
  }, {
    resolve: async () => ({ oauthClientId: 'synthetic-client', oauthClientSecret: new SecretString('synthetic-secret'), accessToken: new SecretString('synthetic-workday-token'), refreshToken: new SecretString('synthetic-refresh') }),
  });
  const connection = newConnection(provider, { provider: 'workday', name: ConnectionName });
  const flow = new WorkerFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `workday-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { workerId: '0123456789abcdef0123456789abcdef' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<WorkerSummary>()).toEqual({ id: '0123456789abcdef0123456789abcdef', descriptor: 'Ada Lovelace' });
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
