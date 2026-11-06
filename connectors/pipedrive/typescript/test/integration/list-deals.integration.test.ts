// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { PipedriveClient } from '../../src/client.js';
import { newConnection, type ObjectPage } from '../../src/index.js';
import { ConnectionName, ListDealsFlow } from '../../examples/list-deals/flow/workflow.js';

it('lists deals through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const paths: string[] = [];
  const server = createServer((request, response) => {
    paths.push(`${request.method} ${request.url}`);
    if (request.method !== 'GET' || !request.url?.startsWith('/api/v2/deals?')) { response.writeHead(404).end(); return; }
    if (request.headers['x-api-token'] !== 'pipedrive-unit-test-api-token') { response.writeHead(401).end(); return; }
    response.setHeader('Content-Type', 'application/json');
    if (request.url.includes('cursor=REJECT')) { response.writeHead(403).end('{"success":false}'); return; }
    response.writeHead(200).end('{"success":true,"data":[{"id":301,"title":"Acme renewal","status":"open"}]}');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Pipedrive fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-pipedrive-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'pipedrive', name: ConnectionName };
  const provider = new PipedriveClient({ endpoint: `http://127.0.0.1:${address.port}` }, new StaticCredentialProvider([[connection, { authMethodId: 'api-token', apiToken: new SecretString('pipedrive-unit-test-api-token') }]]));
  const flow = new ListDealsFlow(newConnection(provider, connection));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `pipedrive-list-${randomUUID()}`;
    await client.startFlow(flow, successId, { objectType: 'deals', limit: 2 });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<ObjectPage>().objects?.[0]?.id).toBe('301');
    const rejectionId = `pipedrive-list-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { objectType: 'deals', limit: 2, cursor: 'REJECT' });
    const rejected = await client.waitForFlow(rejectionId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(paths).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
