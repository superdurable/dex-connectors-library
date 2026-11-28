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
import { WorkerListFlow } from '../../examples/worker-inspection/list-flow.js';
import type { WorkerPage } from '../../src/index.js';

it('retries a provider 503 and persists the worker through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const workerId = '65699281fb7bc7c3510f44a6';
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer sample-rippling-api-token') { response.writeHead(401).end(); return; }
    const url = new URL(request.url ?? '/', 'http://127.0.0.1');
    if (url.pathname === '/workers/') {
      const cursor = url.searchParams.get('cursor');
      if (cursor === 'a+/=' && url.searchParams.size === 1) {
        response.writeHead(200).end(JSON.stringify({ results: [] }));
      } else if (url.searchParams.get('limit') === '2' && url.searchParams.size === 1) {
        response.writeHead(200).end(JSON.stringify({
          results: [{ id: 'example-2', title: 'Engineer', status: null, salary: 77 }],
          next_link: 'https://example.invalid/workers/?cursor=a%2B%2F%3D',
        }));
      } else { response.writeHead(400).end(); }
      return;
    }
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
  const pageFlow = new WorkerListFlow(connection);
  const registry = new Registry([flow, pageFlow]);
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
    const firstId = `rippling-first-${randomUUID()}`;
    await client.startFlow(pageFlow, firstId, { limit: 2 });
    const first = await client.waitForFlow(firstId, 30_000);
    expect(first.status, first.errorMessage).toBe('completed');
    const page = first.singleOutput<WorkerPage>();
    expect(page).toEqual({ workers: [{ id: 'example-2', title: 'Engineer', status: '' }], nextCursor: 'a+/=' });
    const lastId = `rippling-last-${randomUUID()}`;
    await client.startFlow(pageFlow, lastId, { cursor: page.nextCursor });
    const last = await client.waitForFlow(lastId, 30_000);
    expect(last.status, last.errorMessage).toBe('completed');
    expect(last.singleOutput<WorkerPage>()).toEqual({ workers: [], nextCursor: '' });
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
