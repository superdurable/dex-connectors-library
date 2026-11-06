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
import { JiraServiceManagementClient } from '../../src/client.js';
import { newConnection, type CustomerMatches } from '../../src/index.js';
import { ConnectionName, FindCustomerFlow } from '../../examples/find-customer/flow/workflow.js';

it('finds a customer and fails an unwired rejection through Dex Worker and Client', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer inert-access-token') { response.writeHead(401).end(); return; }
    if (request.url?.includes('nobody%40example.com')) { response.writeHead(403).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ values: [
      { accountId: 'qm:customer-1', emailAddress: 'jane@example.com', displayName: 'Jane Doe', active: true },
      { accountId: 'qm:customer-2', emailAddress: 'other@example.com', displayName: 'Other', active: true },
    ], isLastPage: true }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('provider stand-in port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-jsm-find-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'atlassian', name: ConnectionName };
  const provider = new JiraServiceManagementClient({ endpoint: `http://127.0.0.1:${address.port}`, cloudId: '11223344-a1b2-4b33-8c44-def123456789' }, new StaticCredentialProvider([[connection, {
    accessToken: new SecretString('inert-access-token'), refreshToken: new SecretString('inert-refresh-token'), oauthClientId: 'inert-client-id', oauthClientSecret: new SecretString('inert-client-secret'),
  }]]));
  const flow = new FindCustomerFlow(newConnection(provider, connection));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const foundID = `jsm-find-${randomUUID()}`;
    await client.startFlow(flow, foundID, { serviceDeskId: '10', email: 'jane@example.com' });
    const found = await client.waitForFlow(foundID, 60_000);
    expect(found.status, found.errorMessage).toBe('completed');
    expect(found.singleOutput<CustomerMatches>().customers?.map(customer => customer.accountId)).toEqual(['qm:customer-1']);
    const rejectedID = `jsm-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedID, { serviceDeskId: '10', email: 'nobody@example.com' });
    const rejected = await client.waitForFlow(rejectedID, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
