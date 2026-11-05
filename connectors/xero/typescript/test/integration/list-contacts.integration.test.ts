// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { XeroClient } from '../../src/client.js';
import { newConnection, type ContactPage } from '../../src/index.js';
import { ConnectionName, ListContactsFlow } from '../../examples/list-contacts/flow/workflow.js';

it('lists contacts through real Dex and fails an unwired Xero rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.method !== 'GET' || !request.url?.startsWith('/api.xro/2.0/Contacts?')) { response.writeHead(404).end(); return; }
    if (request.headers.authorization !== 'Bearer xero-test-access-token-0123456789') { response.writeHead(401).end(); return; }
    if (new URL(request.url, 'http://localhost').searchParams.get('searchTerm') === 'REJECT') { response.writeHead(403, { 'Content-Type': 'application/json' }).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ Contacts: [{ ContactID: '025867f1-d741-4d6b-b1af-9ac774b59ba7', Name: 'City Agency', ContactStatus: 'ACTIVE' }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Xero fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-xero-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials = new StaticCredentialProvider([[{ provider: 'xero', name: ConnectionName }, { authMethodId: 'custom-connection', clientId: '0F1E2D3C4B5A69788796A5B4C3D2E1F0', clientSecret: new SecretString('client-secret'), accessToken: new SecretString('xero-test-access-token-0123456789') }]]);
  const provider = new XeroClient({}, credentials, { localProviderURL: `http://127.0.0.1:${address.port}` });
  const flow = new ListContactsFlow(newConnection(provider, { provider: 'xero', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `xero-contacts-${randomUUID()}`;
    await client.startFlow(flow, successId, { searchTerm: 'Agency', pageSize: 2 });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<ContactPage>().contacts?.[0]?.name).toBe('City Agency');
    const rejectionId = `xero-contacts-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { searchTerm: 'REJECT', pageSize: 2 });
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
