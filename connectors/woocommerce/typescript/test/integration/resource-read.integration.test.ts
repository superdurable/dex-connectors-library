// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { newConnection, WooCommerceClient, type OrderPage } from '../../src/index.js';
import { ConnectionName, ResourceReadFlow } from '../../examples/resource-read/flow/workflow.js';

it('runs the example through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Basic Y2tfdGVzdDpjc190ZXN0') { response.writeHead(401).end(); return; }
    if (request.url?.includes('page=404')) { response.writeHead(404).end('{"error":"SENTINEL"}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end('[{"id":101,"status":"processing","total":"29.00","currency":"USD"}]');
  });
  await new Promise<void>((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve); });
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('fixture has no TCP port');
  const directory = await mkdtemp(join(tmpdir(), 'dex-woocommerce-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new WooCommerceClient({ storeUrl: `http://127.0.0.1:${address.port}/shop` }, { resolve: async () => ({ consumerKey: new SecretString('ck_test'), consumerSecret: new SecretString('cs_test') }) });
  const flow = new ResourceReadFlow(newConnection(provider, { provider: 'woocommerce', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `woocommerce-read-${randomUUID()}`;
    await client.startFlow(flow, successId, { page: 2, perPage: 10 });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<OrderPage>().orders?.[0]?.id).toBe(101);
    const missingId = `woocommerce-missing-${randomUUID()}`;
    await client.startFlow(flow, missingId, { page: 404 });
    const missing = await client.waitForFlow(missingId, 60_000);
    expect(missing.status).toBe('failed');
    expect(missing.errorMessage).toContain('notFound');
    expect(requests).toEqual(['GET /shop/wp-json/wc/v3/orders?page=2&per_page=10', 'GET /shop/wp-json/wc/v3/orders?page=404']);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
