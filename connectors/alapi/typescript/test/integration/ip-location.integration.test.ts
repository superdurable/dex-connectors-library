// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { expect, it } from 'vitest';
import { ALAPIClient } from '../../src/client.js';
import { newConnection, type LookupIPLocationOutput } from '../../src/index.js';
import { ConnectionName, IPLocationFlow } from '../../examples/ip-location/flow.js';

it('retries ALAPI 503 through a real Dex Worker and persists the location', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'POST' || request.url !== '/api/ip' || request.headers.token !== 'test-token') { response.writeHead(400).end(); return; }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ success: true, code: 200, data: { country: 'US', province: 'CA', city: 'Mountain View', district: '', isp: 'example' } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('ALAPI fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-alapi-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new ALAPIClient({}, { resolve: async () => ({ token: new SecretString('test-token') }) }, {
    send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}/api/ip`, addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new IPLocationFlow(newConnection(provider, { provider: 'alapi', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `alapi-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { ipAddress: '8.8.8.8' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<LookupIPLocationOutput>()).toEqual({ country: 'US', province: 'CA', city: 'Mountain View', district: '', operator: 'example' });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
