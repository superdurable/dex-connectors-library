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
import { FreshBooksClient } from '../../src/client.js';
import { newConnection, type Invoice, type InvoicePage } from '../../src/index.js';
import { ConnectionName, InvoiceFlow } from '../../examples/invoice/flow.js';

import { InvoicePageFlow } from '../../examples/invoice/list-flow.js';

it('retries a provider 503 and persists invoice through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer synthetic-freshbooks-token') { response.writeHead(401).end(); return; }
    const requestURL = new URL(request.url ?? '', 'http://127.0.0.1');
    if (requestURL.pathname === '/accounting/account/KAG77/invoices/invoices' && requestURL.search === '?page=2&per_page=100') {
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ response: { result: {page: 2, pages: 2, total: 100, invoices: []} } }));
      return;
    }
    if (requestURL.pathname !== '/accounting/account/KAG77/invoices/invoices/2201278') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ response: { result: { invoice: { accountid: 'KAG77', invoiceid: 2201278, invoice_number: '0000003', amount: { amount: '800.00', code: 'USD' }, payment_status: 'unpaid' } } } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Invoice fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-freshbooks-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new FreshBooksClient({ redirectUri: 'https://dex.example/callback', baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => ({ oauthClientId: 'synthetic-client', oauthClientSecret: new SecretString('synthetic-secret'), accessToken: new SecretString('synthetic-freshbooks-token'), refreshToken: new SecretString('synthetic-refresh') }),
  });
  const connection = newConnection(provider, { provider: 'freshbooks', name: ConnectionName });
  const flow = new InvoiceFlow(connection);
  const pageFlow = new InvoicePageFlow(connection);
  const registry = new Registry([flow, pageFlow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `freshbooks-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { accountId: 'KAG77', invoiceId: 2201278 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<Invoice>().invoiceId).toBe(2201278);
    expect(attempts).toBe(2);
    const pageId = `freshbooks-typescript-page-${randomUUID()}`;
    await client.startFlow(pageFlow, pageId, { accountId: 'KAG77', page: 2 });
    const page = await client.waitForFlow(pageId, 60_000);
    expect(page.status, page.errorMessage).toBe('completed');
    expect(page.singleOutput<InvoicePage>()).toEqual({accountId: 'KAG77', invoices: [], page: 2, pageCount: 2, total: 100});
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
