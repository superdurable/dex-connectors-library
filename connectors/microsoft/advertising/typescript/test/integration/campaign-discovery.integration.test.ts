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
import { MicrosoftAdvertisingClient } from '../../src/client.js';
import { newConnection, type CampaignsPage } from '../../src/index.js';
import { ConnectionName, CampaignFlow } from '../../examples/campaign-discovery/flow.js';

it('retries a provider 503 and persists campaigns through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.method !== 'POST' || request.headers.authorization !== 'Bearer synthetic-microsoft-advertising-token' ||
      request.headers.developertoken !== 'synthetic-developer-token' ||
      request.headers.customerid !== '73456' || request.headers.customeraccountid !== '12345') { response.writeHead(401).end(); return; }
    const requestURL = new URL(request.url ?? '', 'http://127.0.0.1');
    if (requestURL.pathname !== '/CampaignManagement/v13/Campaigns/QueryByAccountId') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ Campaigns: [{ Id: '98765', Name: 'Search launch', CampaignType: 'Search', Status: 'Active' }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Campaign fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-microsoft-advertising-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new MicrosoftAdvertisingClient({
    baseUrl: `http://127.0.0.1:${address.port}`, customerId: '73456',
  }, {
    resolve: async () => ({ oauthClientId: 'synthetic-client', oauthClientSecret: new SecretString('synthetic-secret'), developerToken: new SecretString('synthetic-developer-token'), accessToken: new SecretString('synthetic-microsoft-advertising-token'), refreshToken: new SecretString('synthetic-refresh') }),
  });
  const connection = newConnection(provider, { provider: 'microsoft-advertising', name: ConnectionName });
  const flow = new CampaignFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `microsoft-advertising-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { accountId: '12345' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<CampaignsPage>()).toEqual({ campaigns: [{ id: '98765', name: 'Search launch', campaignType: 'Search', status: 'Active' }] });
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
