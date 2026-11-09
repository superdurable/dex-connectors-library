// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Buffer } from 'node:buffer';
import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { BraintreeClient } from '../../src/client.js';
import { newConnection, type TransactionProfile, type Credentials } from '../../src/index.js';
import { ConnectionName, TransactionReadFlow } from '../../examples/transaction-read/flow.js';

it('retries a provider 503 and persists the worker through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const input = { transactionId: 'txn-1' };
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== `Basic ${Buffer.from('sample-public-key:sample-private-key').toString('base64')}`) { response.writeHead(401).end(); return; }
    if (!request.url || new URL(request.url, 'http://localhost').pathname !== '/graphql' || request.method !== 'POST') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
      data: { node: { __typename: 'Transaction', id: 'txn-1', status: 'SETTLED' } },
    }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Braintree fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-braintree-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { publicKey: 'sample-public-key', privateKey: new SecretString('sample-private-key') };
  const provider = new BraintreeClient({ baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => credentials,
  });
  const connection = newConnection(provider, { provider: 'braintree', name: ConnectionName });
  const flow = new TransactionReadFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `braintree-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<TransactionProfile>().status).toBe('SETTLED');
    expect(result.singleOutput<TransactionProfile>().id).toBe('txn-1');
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
