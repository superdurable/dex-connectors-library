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
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { expect, it } from 'vitest';
import { ShopifyClient } from '../../src/client.js';
import { newConnection, type Order } from '../../src/index.js';
import { ConnectionName, GetOrderFlow } from '../../examples/order-read/flow/workflow.js';

it('reads an order through real Dex and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers['x-shopify-access-token'] !== 'test-token') { response.writeHead(401).end(); return; }
    if (request.url?.endsWith('/orders/102.json')) { response.writeHead(403).end('{}'); return; }
    if (request.method !== 'GET' || request.url !== '/admin/api/2026-10/orders/101.json') { response.writeHead(404).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ order: { id: 101, name: '#101', financial_status: 'paid', total_price: '29.00', currency: 'USD' } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Shopify fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-shopify-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new ShopifyClient({ shopSubdomain: 'test-shop' }, {
    resolve: async () => ({ accessToken: new SecretString('test-token') }),
  }, { send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}${new URL(request.url).pathname}`, addressPolicy: 'privateNetworkHTTP' }) });
  const flow = new GetOrderFlow(newConnection(provider, { provider: 'shopify', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `shopify-order-${randomUUID()}`;
    await client.startFlow(flow, successId, { orderId: 101 });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<Order>().id).toBe(101);
    const rejectionId = `shopify-order-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { orderId: 102 });
    const rejected = await client.waitForFlow(rejectionId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
