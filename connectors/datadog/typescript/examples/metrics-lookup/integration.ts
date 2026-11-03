// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { DatadogClient, newConnection, type MetricsQuery } from '../../src/index.js';
import { ConnectionName, MetricsLookupFlow } from './flow.js';

/** Exercise the checked-in Flow through a real Dex Worker and Client. */
export async function verifyMetricsLookup(serverAddress: string): Promise<void> {
  let requestCount = 0;
  const provider = createServer((request, response) => {
    requestCount++;
    assert.equal(request.headers['dd-api-key'], 'test-apikey');
    assert.equal(request.headers['dd-application-key'], 'test-applicationkey');
    assert.equal(new URL(request.url!, 'http://localhost').pathname, '/api/v1/query');
    if (request.url?.includes('reject')) { response.writeHead(401).end('{}'); return; }
    if (requestCount === 1) { response.writeHead(429, { 'Retry-After': '1' }).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end('{"status":"ok","series":[{"metric":"system.cpu.user","pointlist":[[1761436800000,0.23]]}]}');
  });
  provider.listen(0, '127.0.0.1');
  await once(provider, 'listening');
  const address = provider.address();
  if (!address || typeof address === 'string') throw new Error('Datadog stand-in port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-datadog-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const providerClient = new DatadogClient({ baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => ({ apiKey: new SecretString('test-apikey'), applicationKey: new SecretString('test-applicationkey') }),
  });
  const flow = new MetricsLookupFlow(newConnection(providerClient, { provider: 'datadog', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const input = { from: 1761436800, to: 1761440400, query: 'system.cpu.user{*}' };
    const successId = `datadog-success-${randomUUID()}`;
    await client.startFlow(flow, successId, input);
    const success = await client.waitForFlow(successId, 60_000);
    assert.equal(success.status, 'completed', success.errorMessage);
    assert.equal(success.singleOutput<MetricsQuery>().series?.[0]?.metric, 'system.cpu.user');
    const rejectedId = `datadog-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { ...input, query: 'reject' });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    assert.equal(rejected.status, 'failed');
    assert.match(rejected.errorMessage ?? '', /providerRejected/);
    assert.equal(requestCount, 3);
    console.log('Datadog Dex integration verified: retry, success, and rejection');
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => provider.close(() => resolve()));
  }
}

const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
if (!serverAddress) throw new Error('DEX_FLOW_SERVICE_ADDRESS is required for the integration check');
await verifyMetricsLookup(serverAddress);
