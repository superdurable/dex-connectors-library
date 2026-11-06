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
import { HubSpotClient } from '../../src/client.js';
import { newConnection, type ObjectPage } from '../../src/index.js';
import { ConnectionName, SearchDealsFlow } from '../../examples/search-deals/flow/workflow.js';

it('searches a provider page through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer(async (request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer pat-na1-test-token') { response.writeHead(401).end(); return; }
    let body = '';
    for await (const chunk of request) body += String(chunk);
    const query = JSON.parse(body) as { query?: string };
    if (query.query === 'denied') { response.writeHead(403).end('{"category":"MISSING_SCOPES"}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ total: 1, results: [{ id: '9001', properties: { dealname: 'Opportunity' }, archived: false }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('HubSpot stand-in port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-hubspot-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'hubspot', name: ConnectionName };
  const provider = new HubSpotClient({ endpoint: `http://127.0.0.1:${address.port}` }, new StaticCredentialProvider([[connection, { authMethodId: 'private-app-token', accessToken: new SecretString('pat-na1-test-token') }]]));
  const flow = new SearchDealsFlow(newConnection(provider, connection));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `hubspot-search-${randomUUID()}`;
    await client.startFlow(flow, successId, { objectType: 'deals', query: 'Opportunity', limit: 2 });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<ObjectPage>().objects?.[0]?.id).toBe('9001');
    const rejectedId = `hubspot-denied-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { objectType: 'deals', query: 'denied', limit: 2 });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
