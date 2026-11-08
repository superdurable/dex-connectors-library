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
import { PlaidClient } from '../../src/client.js';
import { newConnection, type AccountsSnapshot, type Credentials } from '../../src/index.js';
import { ConnectionName, AccountsInspectionFlow } from '../../examples/accounts-inspection/flow.js';

it('retries a provider 503 and persists the accounts through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const itemId = 'item_123456';
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers['plaid-client-id'] !== 'sample-plaid-client-id' || request.headers['plaid-secret'] !== 'sample-plaid-secret') { response.writeHead(401).end(); return; }
    if (request.method !== 'POST' || request.url !== '/api/accounts/get') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
      item: { item_id: itemId }, accounts: [{ account_id: 'account_123456', type: 'depository', subtype: 'checking' }],
    }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Plaid fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-plaid-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = {
    clientId: new SecretString('sample-plaid-client-id'), secret: new SecretString('sample-plaid-secret'),
    accessToken: new SecretString('sample-plaid-access-token'),
  };
  const provider = new PlaidClient({ baseUrl: `http://127.0.0.1:${address.port}/api` }, {
    resolve: async () => credentials,
  });
  const connection = newConnection(provider, { provider: 'plaid', name: ConnectionName });
  const flow = new AccountsInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `plaid-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { itemId });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<AccountsSnapshot>().itemId).toBe(itemId);
    expect(result.singleOutput<AccountsSnapshot>().accounts).toEqual([{ id: 'account_123456', type: 'depository', subtype: 'checking' }]);
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
