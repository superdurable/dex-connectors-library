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
import { ConnectionName, TeamCatalogFlow } from '../../examples/team-list/flow.js';
import { RainbondClient } from '../../src/client.js';
import { newConnection, type ListTeamsOutput } from '../../src/index.js';

it('retries Rainbond 503 through a real Dex Worker and persists the team list', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'GET' || request.url !== '/openapi/v1/teams?page=1&page_size=20' || request.headers.authorization !== 'test-key' || request.headers.accept !== 'application/json') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ total: 1, tenants: [{ tenant_id: 'team-1', tenant_name: 'Engineering', is_active: true }], privateMessage: 'test-key' }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Rainbond fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-rainbond-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new RainbondClient({ baseUrl: 'https://console.example.test' }, { resolve: async () => ({ apiKey: new SecretString('test-key') }) }, {
    send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}/openapi/v1/teams?page=1&page_size=20`, addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new TeamCatalogFlow(newConnection(provider, { provider: 'rainbond', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `rainbond-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { page: 1, pageSize: 20 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<ListTeamsOutput>()).toEqual({ teams: [{ id: 'team-1', name: 'Engineering', active: true }], total: 1 });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
