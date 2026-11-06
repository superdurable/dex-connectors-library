// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client as DexClient, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { Client as SquareClient } from '../../src/client.js';
import { newConnection, type Payment } from '../../src/index.js';
import { ConnectionName, ResourceReadFlow } from '../../examples/resource-read/flow/workflow.js';

it('reads a payment through real Dex and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer test-token' || request.headers['square-version'] !== '2026-09-16') { response.writeHead(401).end(); return; }
    if (request.url === '/v2/payments/rejected') { response.writeHead(403).end(); return; }
    if (request.url !== '/v2/payments/pay_101') { response.writeHead(404).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ payment: { id: 'pay_101', status: 'COMPLETED', amount_money: { amount: 2900, currency: 'USD' } } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Square fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-square-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new SquareClient({ baseUrl: `http://127.0.0.1:${address.port}/v2` }, { resolve: async () => ({ accessToken: new SecretString('test-token') }) });
  const flow = new ResourceReadFlow(newConnection(provider, { provider: 'square', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new DexClient(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `square-read-${randomUUID()}`;
    await client.startFlow(flow, successId, { paymentId: 'pay_101' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<Payment>().id).toBe('pay_101');
    const rejectionId = `square-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { paymentId: 'rejected' });
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
