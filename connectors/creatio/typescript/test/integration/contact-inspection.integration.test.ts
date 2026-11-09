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
import { CreatioClient } from '../../src/client.js';
import { newConnection, type ContactSummary, type Credentials } from '../../src/index.js';
import { ConnectionName, ContactInspectionFlow } from '../../examples/contact-inspection/flow.js';

it('retries a provider 503 and persists the contact through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const contactId = '90a68db5-cb3a-4f64-bb60-c0a934ee7600';
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer sample-access-token') { response.writeHead(401).end(); return; }
    const requestURL = new URL(request.url ?? '', 'http://127.0.0.1');
    if (requestURL.pathname !== '/0/odata/Contact' || requestURL.searchParams.get('$filter') !== `Id eq ${contactId}` ||
      requestURL.searchParams.get('$select') !== 'Id,Name,Email' || requestURL.searchParams.get('$top') !== '1') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ value: [{ Id: contactId, Name: 'Ada Lovelace', Email: 'ada@example.com' }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Creatio fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-creatio-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { clientId: 'sample-client-id', clientSecret: new SecretString('sample-client-secret'), accessToken: new SecretString('sample-access-token') };
  const provider = new CreatioClient({ baseUrl: `http://127.0.0.1:${address.port}`, tokenEndpoint: `http://127.0.0.1:${address.port}/connect/token` }, new StaticCredentialProvider([[{ provider: 'creatio', name: ConnectionName }, credentials]]));
  const connection = newConnection(provider, { provider: 'creatio', name: ConnectionName });
  const flow = new ContactInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `creatio-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { contactId });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<ContactSummary>().id).toBe(contactId);
    expect(result.singleOutput<ContactSummary>()).toEqual({ id: contactId, name: 'Ada Lovelace', email: 'ada@example.com' });
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
