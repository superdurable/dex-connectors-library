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
import { MicrosoftDynamics365SalesClient } from '../../src/client.js';
import { newConnection, type Contact, type Credentials } from '../../src/index.js';
import { ConnectionName, ContactFlow } from '../../examples/contact-lookup/flow.js';

it('retries a Dataverse 503 and persists the contact through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.url === '/token' && request.method === 'POST') { response.writeHead(200, { 'Content-Type': 'application/json' }).end('{"access_token":"test-access-token","token_type":"Bearer"}'); return; }
    if (request.method !== 'GET' || !request.url?.startsWith('/api/data/v9.2/contacts?') || request.headers.authorization !== 'Bearer test-access-token') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end('{"value":[{"contactid":"33333333-3333-4333-8333-333333333333","fullname":"Ada Example","emailaddress1":"ada@example.com"}]}');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Microsoft Dynamics 365 Sales fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-dynamics365sales-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { clientSecret: new SecretString('test-client-secret') };
  const provider = new MicrosoftDynamics365SalesClient({ environmentUrl: `http://127.0.0.1:${address.port}`, tenantId: '11111111-1111-4111-8111-111111111111', clientId: '22222222-2222-4222-8222-222222222222' }, {
    resolve: async () => credentials,
  }, { tokenEndpoint: `http://127.0.0.1:${address.port}/token` });
  const connection = newConnection(provider, { provider: 'microsoft-dynamics-365-sales', name: ConnectionName });
  const flow = new ContactFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `dynamics365sales-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { emailAddress: 'ada@example.com' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<Contact>().contactId).toBe('33333333-3333-4333-8333-333333333333');
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
