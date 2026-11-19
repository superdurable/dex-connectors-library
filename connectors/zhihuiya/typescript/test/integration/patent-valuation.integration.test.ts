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
import { ConnectionName, PatentValuationFlow } from '../../examples/patent-valuation/flow.js';
import { ZhihuiyaClient } from '../../src/client.js';
import { newConnection, type GetPatentValuationOutput } from '../../src/index.js';

it('retries Zhihuiya 503 through a real Dex Worker and persists the patent valuation', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'GET' || request.url !== '/patent-valuation-data/patent_valuation?patent_number=JP6012277B2' || request.headers.authorization !== 'Bearer sk-private' || request.headers.accept !== 'application/json') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ status: true, error_code: 0, data: [{ pn: 'JP6012277B2', patent_id: 'patent-1', patsnap_patent_valuation: { patent_value: 43000, patsnap_legal: 71, patsnap_market: 18, patsnap_economy: 23, patsnap_strategic: 71, patsnap_technology: 28 } }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Zhihuiya fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-zhihuiya-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new ZhihuiyaClient({}, { resolve: async () => ({ apiKey: new SecretString('sk-private') }) }, {
    send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}/patent-valuation-data/patent_valuation?patent_number=JP6012277B2`, addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new PatentValuationFlow(newConnection(provider, { provider: 'zhihuiya', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `zhihuiya-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { publicationNumber: 'JP6012277B2' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<GetPatentValuationOutput>()).toEqual({ publicationNumber: 'JP6012277B2', patentID: 'patent-1', patentValueUSD: 43000, legalScore: 71, marketScore: 18, economyScore: 23, strategicScore: 71, technologyScore: 28 });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
