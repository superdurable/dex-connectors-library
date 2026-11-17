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
import { AmapClient } from '../../src/client.js';
import { newConnection, type GeocodeAddressOutput } from '../../src/index.js';
import { ConnectionName, AddressLookupFlow } from '../../examples/address-lookup/flow.js';

it('retries Amap 503 through a real Dex Worker and persists geocoded locations', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'GET' || !request.url?.startsWith('/v3/geocode/geo?') || new URL(request.url, 'http://localhost').searchParams.get('key') !== 'test-amap-key-123' || request.headers.accept !== 'application/json') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
      status: '1', infocode: '10000', count: '1', geocodes: [{ formatted_address: '北京市朝阳区阜通东大街6号', province: '北京市', city: [], district: '朝阳区', adcode: '110105', location: '116.481488,39.990464', level: '门牌号' }],
    }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Amap fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-amap-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new AmapClient({}, { resolve: async () => ({ apiKey: new SecretString('test-amap-key-123') }) }, {
    send: request => sendProviderRequest({
      ...request, url: `http://127.0.0.1:${address.port}${new URL(request.url).pathname}${new URL(request.url).search}`,
      addressPolicy: 'privateNetworkHTTP',
    }),
  });
  const flow = new AddressLookupFlow(newConnection(provider, { provider: 'amap', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `amap-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { address: '北京市朝阳区阜通东大街6号', city: '北京' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<GeocodeAddressOutput>()).toEqual({ locations: [{ formattedAddress: '北京市朝阳区阜通东大街6号', province: '北京市', city: '', district: '朝阳区', adcode: '110105', longitude: 116.481488, latitude: 39.990464, level: '门牌号' }] });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
