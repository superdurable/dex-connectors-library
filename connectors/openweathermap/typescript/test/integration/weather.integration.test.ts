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
import { OpenWeatherMapClient } from '../../src/client.js';
import { newConnection, type CurrentWeatherOutput } from '../../src/index.js';
import { ConnectionName, WeatherFlow } from '../../examples/weather/flow/workflow.js';

it('reads weather through real Dex and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    const url = new URL(request.url ?? '/', 'http://localhost');
    requests.push(`${request.method} ${url.pathname}`);
    if (request.method !== 'GET' || url.pathname !== '/data/2.5/weather' || url.searchParams.get('appid') !== 'test-key-123') {
      response.writeHead(404).end(); return;
    }
    if (url.searchParams.get('lat') === '90') { response.writeHead(403).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ name: 'Meridian', main: { temp: 21.5 }, weather: [{ description: 'clear sky' }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('provider fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-openweathermap-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new OpenWeatherMapClient({ baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => ({ apiKey: new SecretString('test-key-123') }),
  });
  const flow = new WeatherFlow(newConnection(provider, { provider: 'openweathermap', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const input = { latitude: 37.7, longitude: -122.4, units: 'metric' };
    const successId = `openweathermap-weather-${randomUUID()}`;
    await client.startFlow(flow, successId, input);
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<CurrentWeatherOutput>().temperature).toBe(21.5);
    const rejectionId = `openweathermap-weather-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { ...input, latitude: 90 });
    const rejected = await client.waitForFlow(rejectionId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
