// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { createServer } from 'node:http';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { once } from 'node:events';
import { afterAll, it } from 'vitest';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { BaiduMapsClient, newConnection, type GeocodeAddressOutput } from '../../src/index.js';
import { BaiduMapsAddressLookupFlow, ConnectionName } from '../../examples/address-lookup/flow.js';

const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801';
const responseBody = JSON.stringify({ status: 0, result: { location: { lng: 116.30783584945, lat: 40.056583335558 }, precise: 1, confidence: 80, comprehension: 100, level: '门址' } });
let retryAttempts = 0;
const transport = createServer((request, response) => {
  assert.equal(request.url?.split('?')[0], '/geocoding/v3/');
  const query = new URL(request.url || '', 'http://localhost').searchParams;
  assert.equal(query.get('ak'), 'test-baidu-server-ak-123');
  assert.equal(query.get('address'), '北京市海淀区上地十街10号');
  response.setHeader('Content-Type', 'application/json');
  if (query.get('city') === 'retry' && retryAttempts++ === 0) { response.statusCode = 503; response.end('{"status":1}'); return; }
  if (query.get('city') === 'reject') { response.end('{"status":5}'); return; }
  response.end(responseBody);
});
afterAll(() => transport.close());

it('runs the checked-in Flow through a real Dex Worker and Client', async () => {
  transport.listen(0, '127.0.0.1');
  await once(transport, 'listening');
  const address = transport.address();
  if (!address || typeof address === 'string') throw new TypeError('local provider address is unavailable');
  const reference = { provider: 'baidu-maps', name: ConnectionName };
  const credentials = new StaticCredentialProvider([[reference, { accessKey: new SecretString('test-baidu-server-ak-123') }]]);
  const flow = new BaiduMapsAddressLookupFlow(newConnection(new BaiduMapsClient({ baseUrl: `http://127.0.0.1:${address.port}` }, credentials), reference));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-baidu-maps-ts-'));
  const blobCache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const worker = new Worker(registry, blobCache, { serverAddress });
  const client = new Client(registry, blobCache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });
    const flowId = `baidu-maps-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { address: '北京市海淀区上地十街10号', city: '北京市' });
    const result = await client.waitForFlow(flowId, 30_000);
    assert.equal(result.status, 'completed', result.errorMessage);
    const output = result.singleOutput<GeocodeAddressOutput>();
    assert.equal(output.longitude, 116.30783584945);
    assert.equal(output.precise, true);

    const retryId = `baidu-maps-retry-${randomUUID()}`;
    await client.startFlow(flow, retryId, { address: '北京市海淀区上地十街10号', city: 'retry' });
    const retried = await client.waitForFlow(retryId, 30_000);
    assert.equal(retried.status, 'completed', retried.errorMessage);
    assert.equal(retryAttempts, 2);

    const rejectedId = `baidu-maps-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { address: '北京市海淀区上地十街10号', city: 'reject' });
    const rejected = await client.waitForFlow(rejectedId, 30_000);
    assert.equal(rejected.status, 'failed');
    assert.match(rejected.errorMessage ?? '', /providerRejected/);
    assert.match(rejected.errorMessage ?? '', /AUTHENTICATION/);
  } finally {
    await client.close();
    await worker.close();
    blobCache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 45_000);
