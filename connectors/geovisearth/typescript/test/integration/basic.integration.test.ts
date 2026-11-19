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
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { expect, it } from 'vitest';
import { GeovisearthClient } from '../../src/client.js';
import { newConnection, type Credentials } from '../../src/index.js';
import { ConnectionName, GeovisearthInspectionFlow } from '../../examples/basic/flow.js';

it('retries a provider 503 and persists the result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((_request, response) => {
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ status: 0, date: { time: '20240911135031' }, result: { wp: '阴', wp_code: '02', tem: 26.7, rh: 95, wd_desc: '东北风', ws_desc: '3级', prs: 986.7 }, location: { areaCode: 'WTX_CH101300201' } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Geovisearth fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-geovisearth-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { token: new SecretString('private-test-token') };
  const provider = new GeovisearthClient({}, { resolve: async () => credentials }, { send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}/v2/cn/area/basic${new URL(request.url).search}`, addressPolicy: 'privateNetworkHTTP' }) });
  const connection = newConnection(provider, { provider: 'geovisearth', name: ConnectionName });
  const flow = new GeovisearthInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `geovisearth-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { location: 'WTX_CH101300201' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({ areaCode: 'WTX_CH101300201', weather: '阴', weatherCode: '02', temperatureCelsius: 26.7, relativeHumidityPercent: 95, windDirection: '东北风', windForce: '3级', surfacePressureHpa: 986.7, observedAt: '20240911135031' });
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
