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
import { ConnectionName, WeatherLookupFlow } from '../../examples/current-weather/flow.js';
import { SeniverseClient } from '../../src/client.js';
import { newConnection, type GetCurrentWeatherOutput } from '../../src/index.js';

it('retries Seniverse 503 through a real Dex Worker and persists the current weather', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'GET' || request.url !== '/v3/weather/now.json?key=test-key&location=%E5%8C%97%E4%BA%AC&language=zh-Hans&unit=c' || request.headers.accept !== 'application/json') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ results: [{ location: { id: 'WX4FBXXFKE4F', name: '北京', country: 'CN' }, now: { text: '晴', code: '0', temperature: '20' }, last_update: '2026-01-01T12:00:00+08:00' }], privateMessage: 'test-key' }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Seniverse fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-seniverse-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new SeniverseClient({ baseUrl: 'https://api.seniverse.com' }, { resolve: async () => ({ privateKey: new SecretString('test-key') }) }, {
    send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}/v3/weather/now.json${new URL(request.url).search}`, addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new WeatherLookupFlow(newConnection(provider, { provider: 'seniverse', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `seniverse-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { location: '北京' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<GetCurrentWeatherOutput>()).toEqual({ locationId: 'WX4FBXXFKE4F', locationName: '北京', country: 'CN', weatherText: '晴', weatherCode: '0', temperature: '20', temperatureUnit: 'c', lastUpdate: '2026-01-01T12:00:00+08:00' });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
