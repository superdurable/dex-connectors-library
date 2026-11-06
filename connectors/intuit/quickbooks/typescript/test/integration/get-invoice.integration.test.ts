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
import { QuickBooksClient } from '../../src/client.js';
import { newConnection, type Invoice } from '../../src/index.js';
import { ConnectionName, GetQuickBooksInvoiceFlow } from '../../examples/get-invoice/flow/workflow.js';

it('reads an invoice through real Dex and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer quickbooks-test-token') { response.writeHead(401).end(); return; }
    if (request.url === '/v3/company/9341453050298464/invoice/130?minorversion=75') {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ Invoice: { Id: '130', CustomerRef: { value: '58' }, TotalAmt: 99.9, Balance: 0, Line: [] } }));
    } else response.writeHead(404).end();
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('QuickBooks fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-quickbooks-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials = { accessToken: new SecretString('quickbooks-test-token'), clientId: 'test-client', clientSecret: new SecretString('test-secret'), refreshToken: new SecretString(''), idToken: new SecretString('') };
  const source = { resolve: async () => credentials, resolveWithRefresh: async () => credentials };
  const provider = new QuickBooksClient({ realmId: '9341453050298464' }, source, { localProviderURL: `http://127.0.0.1:${address.port}` });
  const flow = new GetQuickBooksInvoiceFlow(newConnection(provider, { provider: 'quickbooks', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successID = `quickbooks-invoice-${randomUUID()}`;
    await client.startFlow(flow, successID, { invoiceId: '130' });
    const success = await client.waitForFlow(successID, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<Invoice>().invoiceId).toBe('130');
    const rejectedID = `quickbooks-invoice-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedID, { invoiceId: '131' });
    const rejected = await client.waitForFlow(rejectedID, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('notFound');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
