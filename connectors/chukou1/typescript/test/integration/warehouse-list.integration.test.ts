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
import { Chukou1Client, newConnection, type WarehousePage } from '../../src/index.js';
import { Chukou1WarehouseListFlow, ConnectionName, warehousesAttribute } from '../../examples/warehouse-list/flow.js';

it('retries a 503 through a real Dex Worker and persists the warehouse list', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    if (request.url !== '/v1/warehouses' || request.method !== 'GET') { response.writeHead(404).end(); return; }
    if (request.headers.authorization !== 'Bearer secretTestToken') { response.writeHead(401).end(); return; }
    requests++;
    if (requests === 1) { response.writeHead(503, { 'Retry-After': '1' }).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify([{ WarehouseId: 'US', WarehouseName: 'San Francisco Warehouse', Country: 'United States', CountryCode: 'US' }]));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-chukou1-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new Chukou1Client({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ accessToken: new SecretString('secretTestToken') }) });
  const flow = new Chukou1WarehouseListFlow(newConnection(provider, { provider: 'chukou1', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `chukou1-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const expected: WarehousePage = { warehouses: [{ id: 'US', name: 'San Francisco Warehouse', country: 'United States', countryCode: 'US' }] };
    expect(result.singleOutput<WarehousePage>()).toEqual(expected);
    const [persisted] = await client.getAttributes(flowId, warehousesAttribute);
    expect(persisted).toEqual(expected);
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
