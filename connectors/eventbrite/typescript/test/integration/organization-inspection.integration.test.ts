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
import { EventbriteClient } from '../../src/client.js';
import { newConnection, type OrganizationsPage } from '../../src/index.js';
import { ConnectionName, OrganizationInspectionFlow } from '../../examples/organization-inspection/flow.js';

it('retries a provider 503 and persists the organization page through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer synthetic-private-token') { response.writeHead(401).end(); return; }
    if (request.url !== '/v3/users/me/organizations/?page=1') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
      organizations: [{ id: '12345', name: 'Example organization' }],
      pagination: { page_number: 1, page_count: 1 },
    }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Eventbrite fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-eventbrite-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new EventbriteClient({ baseUrl: `http://127.0.0.1:${address.port}/v3` }, {
    resolve: async () => ({ privateToken: new SecretString('synthetic-private-token') }),
  });
  const connection = newConnection(provider, { provider: 'eventbrite', name: ConnectionName });
  const flow = new OrganizationInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `eventbrite-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { page: 1 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<OrganizationsPage>().organizations?.[0]?.id).toBe('12345');
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
