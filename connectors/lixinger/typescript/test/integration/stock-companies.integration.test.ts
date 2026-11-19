// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { SecretString } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { ConnectionName, StockCompaniesFlow } from '../../examples/stock-companies/flow.js';
import { LixingerClient } from '../../src/client.js';
import { newConnection, type ListStockCompaniesOutput } from '../../src/index.js';

it('retries Lixinger 503 through a real Dex Worker and persists the observation', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'POST' || request.url !== '/api/cn/company') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ code: 1, data: [{ stockCode: '300750', name: '宁德时代', exchange: 'sz', listingStatus: 'normally_listed' }, { stockCode: '600519', name: '贵州茅台', exchange: 'sh' }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Lixinger fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-lixinger-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new LixingerClient({}, { resolve: async () => ({ token: new SecretString('private-test-token') }) }, {
    send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}${new URL(request.url).pathname}`, addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new StockCompaniesFlow(newConnection(provider, { provider: 'lixinger', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `lixinger-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { stockCodes: ['300750', '600519'] });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<ListStockCompaniesOutput>()).toEqual({ companies: [{ stockCode: '300750', name: '宁德时代', exchange: 'sz', listingStatus: 'normally_listed' }, { stockCode: '600519', name: '贵州茅台', exchange: 'sh', listingStatus: '' }] });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
