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
import { QiniuClient, newConnection, type ListBucketsOutput } from '../../src/index.js';
import { QiniuBucketListFlow, ConnectionName, bucketListAttribute } from '../../examples/bucket-list/flow.js';

const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801';
let requests = 0;
let shouldReject = false;
const provider = createServer((request, response) => {
  if (request.method !== 'GET' || request.url !== '/buckets' || request.headers.accept !== 'application/json' ||
      !request.headers.authorization?.startsWith('Qiniu test-access-key:') || !request.headers['x-qiniu-date']) {
    response.writeHead(401).end(); return;
  }
  requests++;
  if (shouldReject) { response.writeHead(401).end('{}'); return; }
  if (requests === 1) { response.writeHead(503).end(); return; }
  response.writeHead(200).end(JSON.stringify(['demo-images', 'archive-data']));
});
afterAll(() => provider.close());

it('retries the checked-in Flow, persists bucket names, and fails on an unwired rejection', async () => {
  provider.listen(0, '127.0.0.1'); await once(provider, 'listening');
  const address = provider.address();
  if (!address || typeof address === 'string') throw new TypeError('local provider address unavailable');
  const reference = { provider: 'qiniu', name: ConnectionName };
  const credentials = new StaticCredentialProvider([[reference, {
    accessKey: new SecretString('test-access-key'), secretKey: new SecretString('test-secret-key'),
  }]]);
  const connector = new QiniuClient({ baseUrl: 'http://127.0.0.1:' + address.port }, credentials);
  const flow = new QiniuBucketListFlow(newConnection(connector, reference));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-qiniu-ts-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = 'qiniu-ts-' + randomUUID();
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const output: ListBucketsOutput = { buckets: ['demo-images', 'archive-data'] };
    expect(result.singleOutput<ListBucketsOutput>()).toEqual(output);
    expect(await client.getAttributes(flowId, bucketListAttribute)).toEqual([output]);
    expect(requests).toBe(2);
    shouldReject = true;
    const rejectedId = 'qiniu-ts-rejected-' + randomUUID();
    await client.startFlow(flow, rejectedId, {});
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toMatch(/providerRejected/);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
