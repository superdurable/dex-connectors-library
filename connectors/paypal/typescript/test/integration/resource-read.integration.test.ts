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
import { PayPalClient } from '../../src/client.js';
import { newConnection, type Order } from '../../src/index.js';
import { ConnectionName, ResourceReadFlow } from '../../examples/resource-read/flow/workflow.js';

it('reads an order through real Dex and fails an unwired notFound branch', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.url === '/v1/oauth2/token') {
      if (request.headers.authorization !== 'Basic Y2xpZW50LXRlc3Q6c2VjcmV0LXRlc3Q=') { response.writeHead(401).end(); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ access_token: 'short-lived-token', token_type: 'Bearer', expires_in: 3600 }));
      return;
    }
    if (request.headers.authorization !== 'Bearer short-lived-token') { response.writeHead(401).end(); return; }
    if (request.url === '/v2/checkout/orders/MISSING') { response.writeHead(404).end('{}'); return; }
    if (request.url === '/v2/checkout/orders/ORDER101') {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ id: 'ORDER101', status: 'COMPLETED' })); return;
    }
    response.writeHead(404).end();
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('PayPal fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-paypal-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new PayPalClient({ baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => ({ clientId: 'client-test', clientSecret: new SecretString('secret-test') }),
  });
  const flow = new ResourceReadFlow(newConnection(provider, { provider: 'paypal', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `paypal-success-${randomUUID()}`;
    await client.startFlow(flow, successId, { orderId: 'ORDER101' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<Order>().id).toBe('ORDER101');
    const missingId = `paypal-missing-${randomUUID()}`;
    await client.startFlow(flow, missingId, { orderId: 'MISSING' });
    const missing = await client.waitForFlow(missingId, 60_000);
    expect(missing.status).toBe('failed');
    expect(missing.errorMessage).toContain('notFound');
    expect(requests.filter(request => request.startsWith('POST'))).toHaveLength(1);
    expect(requests.filter(request => request.startsWith('GET'))).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
