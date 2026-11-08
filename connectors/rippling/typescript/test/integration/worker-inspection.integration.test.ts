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
import { RipplingClient } from '../../src/client.js';
import { newConnection, type WorkerSummary, type Credentials } from '../../src/index.js';
import { ConnectionName, WorkerInspectionFlow } from '../../examples/worker-inspection/flow.js';

it('retries a provider 503 and persists the worker through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const workerId = '65699281fb7bc7c3510f44a6';
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer sample-rippling-api-token') { response.writeHead(401).end(); return; }
    if (request.url !== `/workers/${encodeURIComponent(workerId)}/`) { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
      id: workerId, status: 'ACTIVE', title: 'Engineer',
    }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Rippling fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-rippling-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { apiToken: new SecretString('sample-rippling-api-token') };
  const provider = new RipplingClient({ baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => credentials,
  });
  const connection = newConnection(provider, { provider: 'rippling', name: ConnectionName });
  const flow = new WorkerInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `rippling-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { workerId });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<WorkerSummary>().id).toBe(workerId);
    expect(result.singleOutput<WorkerSummary>().status).toBe('ACTIVE');
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
