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
import { AccountInfoFlow, ConnectionName } from '../../examples/account-info/flow.js';
import { ApiZeroClient } from '../../src/client.js';
import { newConnection, type GetAccountInfoOutput } from '../../src/index.js';

it('retries ApiZero 503 through a real Dex Worker and persists only the permitted balance', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'GET' || request.url !== '/api/user-info' || request.headers.authorization !== 'Bearer sk_live_test-secret' || request.headers.accept !== 'application/json') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(JSON.stringify({ code: 5000 })); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ code: 0, data: { scope: 'account', balance: { yuan: '123.4000' }, key: { quota_remaining: 99 }, private: 'secret' }, msg: 'secret' }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('ApiZero fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-apizero-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new ApiZeroClient({}, { resolve: async () => ({ apiKey: new SecretString('sk_live_test-secret') }) }, {
    send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}/api/user-info`, addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new AccountInfoFlow(newConnection(provider, { provider: 'apizero', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `apizero-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<GetAccountInfoOutput>()).toEqual({ scope: 'account', balanceYuan: '123.4000' });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
