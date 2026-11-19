// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterAll, expect, it } from 'vitest';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { CaiyunClient, newConnection, type GetCurrentWeatherOutput } from '../../src/index.js';
import { CaiyunCurrentWeatherFlow, ConnectionName, currentWeatherAttribute } from '../../examples/current-weather/flow.js';

const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801';
const nonce = Uint8Array.from([0x01, 0x95, 0xc6, 0x8a, 0x42, 0xe7, 0x72, 0x43, 0xbf, 0xf2, 0xac, 0x97, 0xa7, 0x8b, 0x83, 0x7d]);
let requests = 0;
let isRejecting = false;
const provider = createServer((request, response) => {
  if (request.method !== 'GET' || request.url !== '/v2.6/test-app-key/116.3176,39.976/realtime' ||
      request.headers['x-cy-nonce'] !== '0195c68a42e77243bff2ac97a78b837d' || request.headers['x-cy-timestamp'] !== '1792654000' ||
      request.headers['x-cy-signature'] !== 'StCEDxxdbA5aHTIZYU-2SOaTTd13AE9MPHASxj1iPUw=') { response.writeHead(400).end(); return; }
  requests++;
  if (isRejecting) { response.writeHead(403).end(); return; }
  if (requests === 1) { response.writeHead(503).end(); return; }
  response.writeHead(200).end(JSON.stringify({ status: 'ok', result: { realtime: {
    status: 'ok', temperature: 20, humidity: 0.5, skycon: 'CLEAR_DAY', wind: { speed: 2 },
  } } }));
});
afterAll(() => provider.close());

it('runs the checked-in Flow through Dex, retries, and persists metric weather', async () => {
  provider.listen(0, '127.0.0.1'); await once(provider, 'listening');
  const address = provider.address();
  if (!address || typeof address === 'string') throw new TypeError('local provider address unavailable');
  const reference = { provider: 'caiyun', name: ConnectionName };
  const credentials = new StaticCredentialProvider([[reference, {
    appKey: new SecretString('test-app-key'), appSecret: new SecretString('test-app-secret'),
  }]]);
  const connector = new CaiyunClient({ baseUrl: `http://127.0.0.1:${address.port}` }, credentials, {
    now: () => new Date('2026-10-22T07:26:40Z'), nonce: () => nonce,
  });
  const flow = new CaiyunCurrentWeatherFlow(newConnection(connector, reference));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-caiyun-ts-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `caiyun-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { longitude: 116.3176, latitude: 39.976 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const weather: GetCurrentWeatherOutput = { temperatureCelsius: 20, relativeHumidity: 0.5, skyCondition: 'CLEAR_DAY', windSpeedMetersPerSecond: 2 };
    expect(result.singleOutput<GetCurrentWeatherOutput>()).toEqual(weather);
    expect(await client.getAttributes(flowId, currentWeatherAttribute)).toEqual([weather]);
    expect(requests).toBe(2);
    isRejecting = true;
    const rejectedId = `caiyun-ts-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { longitude: 116.3176, latitude: 39.976 });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toMatch(/providerRejected/);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
