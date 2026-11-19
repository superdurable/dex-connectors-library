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
import { QWeatherClient } from '../../src/client.js';
import { newConnection, type GetCurrentWeatherOutput } from '../../src/index.js';

it('retries QWeather 503 through a real Dex Worker and persists the current weather', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'GET' || request.url !== '/weather/v1/current/39.920000/116.410000' || request.headers['x-qw-api-key'] !== 'test-key' || request.headers.accept !== 'application/json') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ metadata: { attributions: ['https://developer.qweather.com/attribution.html'] }, validTime: '2024-05-31T03:15Z', condition: { text: 'Few Clouds', code: '102' }, temperature: { value: 31.71, unit: '°C' }, privateMessage: 'test-key' }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('QWeather fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-qweather-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new QWeatherClient({}, { resolve: async () => ({ apiHost: new SecretString('abc123.xy.qweatherapi.com'), apiKey: new SecretString('test-key') }) }, {
    send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}${new URL(request.url).pathname}`, addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new WeatherLookupFlow(newConnection(provider, { provider: 'qweather', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `qweather-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { latitude: 39.92, longitude: 116.41 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<GetCurrentWeatherOutput>()).toEqual({ conditionText: 'Few Clouds', conditionCode: '102', temperatureValue: 31.71, temperatureUnit: '°C', validTime: '2024-05-31T03:15Z', attributions: ['https://developer.qweather.com/attribution.html'] });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
