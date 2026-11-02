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
import { BitbucketClient } from '../../src/client.js';
import { newConnection, type RepositoryPage } from '../../src/index.js';
import { ConnectionName, ResourceReadFlow } from '../../examples/resource-read/flow/workflow.js';

it('reads a provider page through a real Dex Worker and fails an unwired 404', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer test-token') { response.writeHead(401).end(); return; }
    if (request.method !== 'GET' || !request.url?.startsWith('/2.0/repositories/')) { response.writeHead(400).end(); return; }
    if (request.url.endsWith('/missing')) { response.writeHead(404).end('{"error":"SENTINEL"}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ values: [{ uuid: '{a1}', name: 'Meridian API', full_name: 'meridian/api', is_private: true }], page: 1, pagelen: 10 }));
  });
  server.listen(21271, '127.0.0.1');
  await once(server, 'listening');
  const directory = await mkdtemp(join(tmpdir(), 'dex-bitbucket-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new BitbucketClient({ baseUrl: 'http://127.0.0.1:21271/2.0' }, {
    resolve: async () => ({ accessToken: new SecretString('test-token') }),
  });
  const flow = new ResourceReadFlow(newConnection(provider, { provider: 'bitbucket', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `bitbucket-read-${randomUUID()}`;
    await client.startFlow(flow, successId, { workspace: 'meridian' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<RepositoryPage>().values?.[0]?.uuid).toBe('{a1}');
    const missingId = `bitbucket-read-missing-${randomUUID()}`;
    await client.startFlow(flow, missingId, { workspace: 'missing' });
    const missing = await client.waitForFlow(missingId, 60_000);
    expect(missing.status).toBe('failed');
    expect(missing.errorMessage).toContain('notFound');
    expect(requests).toEqual(['GET /2.0/repositories/meridian', 'GET /2.0/repositories/missing']);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
