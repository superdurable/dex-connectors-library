// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { SecretString } from '@superdurable/connectors-sdk';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { ConnectionName, CurrentWeatherFlow } from '../../examples/current-weather/flow.js';
import { APISpaceClient } from '../../src/client.js';
import { newConnection, type CurrentWeather } from '../../src/index.js';

it('retries APISpace 503 through a real Dex Worker and persists the observation', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'GET' || request.url !== '/456456/weather/v001/now?areacode=101010100' || request.headers['x-apispace-token'] !== 'test-token' || request.headers['authorization-type'] !== 'apikey' || request.headers['content-type'] !== 'application/json') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ status: 0, result: { location: { name: '北京' }, realtime: { text: '多云', temp: 18.5, rh: 38 }, last_update: '2026-11-13 12:00:00' } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('APISpace fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-apispace-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new APISpaceClient({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ apiToken: new SecretString('test-token') }) });
  const flow = new CurrentWeatherFlow(newConnection(provider, { provider: 'apispace', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `apispace-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { areaCode: '101010100' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<CurrentWeather>()).toEqual({ city: '北京', condition: '多云', temperatureCelsius: 18.5, humidityPercent: 38, updatedAtBeijing: '2026-11-13 12:00:00' });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
