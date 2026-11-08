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
import { CustomerioClient } from '../../src/client.js';
import { newConnection, type CustomerLookupPage, type Credentials } from '../../src/index.js';
import { ConnectionName, CustomerLookupFlow } from '../../examples/customer-lookup/flow.js';

it('retries a provider 503 and persists the worker through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const email = 'person@example.com';
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer sample-customerio-private-key') { response.writeHead(401).end(); return; }
    if (!request.url || new URL(request.url, 'http://localhost').pathname !== '/v1/customers' || new URL(request.url, 'http://localhost').searchParams.get('email') !== email) { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
      results: [{ cio_id: 'a3000001', email: 'person@example.com', id: 2 }],
    }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Customerio fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-customerio-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { privateKey: new SecretString('sample-customerio-private-key') };
  const provider = new CustomerioClient({ baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => credentials,
  });
  const connection = newConnection(provider, { provider: 'customerio', name: ConnectionName });
  const flow = new CustomerLookupFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `customerio-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { email });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<CustomerLookupPage>().email).toBe(email);
    expect(result.singleOutput<CustomerLookupPage>().customerIDs).toEqual(['a3000001']);
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
