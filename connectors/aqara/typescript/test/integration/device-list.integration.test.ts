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
import { ConnectionName, DeviceListFlow } from '../../examples/device-list/flow.js';
import { AqaraClient } from '../../src/client.js';
import { newConnection, type DevicePage } from '../../src/index.js';
import { signDeviceRequest } from '../../src/signature.js';

it('retries signed Aqara 503 through Dex and persists the device page', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const nonces = new Set<string>();
  const server = createServer((request, response) => {
    requests++;
    const nonce = request.headers.nonce;
    const timestamp = request.headers.time;
    if (typeof nonce !== 'string' || typeof timestamp !== 'string') { response.writeHead(400).end(); return; }
    nonces.add(nonce);
    const signature = signDeviceRequest('access-token', 'app-id', 'key-id', nonce, timestamp, 'app-key');
    if (request.method !== 'POST' || request.url !== '/v3.0/open/api' || request.headers.accesstoken !== 'access-token' || request.headers.appid !== 'app-id' || request.headers.keyid !== 'key-id' || request.headers.sign !== signature) {
      response.writeHead(400).end(); return;
    }
    const chunks: Buffer[] = [];
    request.on('data', (chunk: Buffer) => chunks.push(chunk));
    request.on('end', () => {
      const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as unknown;
      if (JSON.stringify(body) !== JSON.stringify({ intent: 'query.device.info', data: { pageNum: 1, pageSize: 50 } })) { response.writeHead(400).end(); return; }
      if (requests === 1) { response.writeHead(503).end(); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ code: 0, result: { totalCount: 1, data: [{ did: 'lumi.1', model: 'lumi.gateway.aqhm01', deviceName: 'Hub', state: 1, modelType: 1, firmwareVersion: '3.2.6' }] } }));
    });
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Aqara fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-aqara-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new AqaraClient({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ appId: 'app-id', keyId: 'key-id', appKey: new SecretString('app-key'), accessToken: new SecretString('access-token') }) });
  const flow = new DeviceListFlow(newConnection(provider, { provider: 'aqara', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `aqara-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { pageNum: 1, pageSize: 50 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<DevicePage>()).toEqual({ totalCount: 1, pageNum: 1, pageSize: 50, devices: [{ deviceId: 'lumi.1', deviceName: 'Hub', model: 'lumi.gateway.aqhm01', modelType: 1, online: true, firmwareVersion: '3.2.6', positionId: '', parentDeviceId: '', timeZone: '' }] });
    expect(requests).toBe(2);
    expect(nonces.size).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
