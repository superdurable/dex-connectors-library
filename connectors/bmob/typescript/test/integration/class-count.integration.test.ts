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
import { BmobClient, newConnection, type CountClassObjectsOutput } from '../../src/index.js';
import { BmobClassCountFlow, ConnectionName, classCountAttribute } from '../../examples/class-count/flow.js';

const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801';
let requests = 0;
const provider = createServer((request, response) => {
  const url = new URL(request.url ?? '', 'http://127.0.0.1');
  if (request.method !== 'GET' || request.headers['x-bmob-application-id'] !== 'test-bmob-app' || request.headers['x-bmob-rest-api-key'] !== 'test-bmob-rest-key') {
    response.writeHead(401).end(); return;
  }
  requests++;
  if (url.pathname.endsWith('/Missing')) { response.writeHead(404).end(); return; }
  if (url.pathname !== '/1/classes/GameScore' || url.searchParams.get('count') !== '1' || url.searchParams.get('limit') !== '0') {
    response.writeHead(400).end(); return;
  }
  if (requests === 1) { response.writeHead(503).end(); return; }
  response.writeHead(200).end(JSON.stringify({ results: [], count: 42 }));
});
afterAll(() => provider.close());

it('runs the checked-in Flow through Dex, retries, and persists the count', async () => {
  provider.listen(0, '127.0.0.1'); await once(provider, 'listening');
  const address = provider.address();
  if (!address || typeof address === 'string') throw new TypeError('local provider address unavailable');
  const reference = { provider: 'bmob', name: ConnectionName };
  const credentials = new StaticCredentialProvider([[reference, {
    applicationId: new SecretString('test-bmob-app'), restApiKey: new SecretString('test-bmob-rest-key'),
  }]]);
  const connector = new BmobClient({ baseUrl: `http://127.0.0.1:${address.port}` }, credentials);
  const flow = new BmobClassCountFlow(newConnection(connector, reference));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-bmob-ts-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `bmob-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { className: 'GameScore' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<CountClassObjectsOutput>()).toEqual({ count: 42 });
    expect(await client.getAttributes(flowId, classCountAttribute)).toEqual([{ count: 42 }]);
    expect(requests).toBe(2);
    const rejectedId = `bmob-ts-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { className: 'Missing' });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toMatch(/providerRejected/);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
