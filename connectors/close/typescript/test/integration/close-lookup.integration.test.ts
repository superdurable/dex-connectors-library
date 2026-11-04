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
import { CloseClient } from '../../src/client.js';
import { newConnection } from '../../src/index.js';
import { ConnectionName, CloseLookupFlow, type CloseLookupOutcome } from '../../examples/close-lookup/flow/workflow.js';

it('lists and fetches a Close lead through real Dex, then fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Basic dGVzdC1zZWNyZXQta2V5Og==') { response.writeHead(401).end(); return; }
    if (request.url === '/api/v1/lead/?_skip=1&_limit=10') {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end('{"data":[{"id":"lead_meridian1","name":"Meridian Labs"}],"has_more":false}');
      return;
    }
    if (request.url === '/api/v1/lead/lead_meridian1/') {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end('{"id":"lead_meridian1","name":"Meridian Labs"}');
      return;
    }
    response.writeHead(403).end();
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const providerAddress = server.address();
  if (!providerAddress || typeof providerAddress === 'string') throw new Error('Close fixture has no TCP address');
  const directory = await mkdtemp(join(tmpdir(), 'dex-close-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new CloseClient({ baseUrl: `http://${providerAddress.address}:${providerAddress.port}/api/v1` }, {
    resolve: async () => ({ apiKey: new SecretString('test-secret-key') }),
  });
  const flow = new CloseLookupFlow(newConnection(provider, { provider: 'close', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `close-lookup-${randomUUID()}`;
    await client.startFlow(flow, flowId, { skip: 1 });
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    expect(completed.singleOutput<CloseLookupOutcome>().lead.id).toBe('lead_meridian1');
    const rejectedId = `close-lookup-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { skip: 2 });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toEqual([
      'GET /api/v1/lead/?_skip=1&_limit=10',
      'GET /api/v1/lead/lead_meridian1/',
      'GET /api/v1/lead/?_skip=2&_limit=10',
    ]);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
