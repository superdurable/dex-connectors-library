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
import { ConnectionName, WaybillBalanceFlow } from '../../examples/waybill-balance/flow.js';
import { YundaClient } from '../../src/client.js';
import { newConnection, type GetWaybillBalanceOutput } from '../../src/index.js';

it('retries Yunda 503 through a real Dex Worker and persists the waybill balance', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'POST' || request.url !== '/openapi-api/v1/accountOrder/searchCount' || request.headers['app-key'] !== '999999' || request.headers['req-time'] !== '1731510000000' || request.headers.accept !== 'application/json') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ code: '0000', result: true, data: { status: '1', free_num: '1', remain_num: '2' } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Yunda fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-yunda-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new YundaClient({}, { resolve: async () => ({ appKey: new SecretString('999999'), appSecret: new SecretString('REDACTED_APP_SECRET'), partnerID: new SecretString('REDACTED_PARTNER_ID'), baimaSecret: new SecretString('REDACTED_BAIMA_SECRET') }) }, {
    now: () => 1731510000000,
    send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}/openapi-api/v1/accountOrder/searchCount`, addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new WaybillBalanceFlow(newConnection(provider, { provider: 'yunda', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `yunda-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<GetWaybillBalanceOutput>()).toEqual({ freeCount: 1, paidCount: 2 });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
