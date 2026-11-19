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
import { ConnectionName, ECSRegionsFlow } from '../../examples/regions/flow.js';
import { AlibabaClient } from '../../src/client.js';
import { newConnection, type DescribeRegionsOutput } from '../../src/index.js';
import { EmptyPayloadSHA256, SignedHeaderNames, signACS3Request } from '../../src/signature.js';

it('retries a signed ECS request through Dex and persists the region list', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const nonces = new Set<string>();
  const server = createServer((request, response) => {
    requests++;
    const host = request.headers.host ?? '';
    const date = request.headers['x-acs-date'];
    const nonce = request.headers['x-acs-signature-nonce'];
    if (typeof date !== 'string' || typeof nonce !== 'string') { response.writeHead(400).end(); return; }
    nonces.add(nonce);
    const signature = signACS3Request('test-access-key-secret', 'GET', host, 'DescribeRegions', '', date, nonce);
    const authorization = `ACS3-HMAC-SHA256 Credential=test-access-key-id,SignedHeaders=${SignedHeaderNames},Signature=${signature}`;
    if (request.method !== 'GET' || request.url !== '/' || request.headers.authorization !== authorization || request.headers['x-acs-content-sha256'] !== EmptyPayloadSHA256 || request.headers['x-acs-version'] !== '2014-05-26') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ Regions: { Region: [{ RegionId: 'cn-hangzhou', LocalName: '华东1（杭州）', RegionEndpoint: 'ecs.cn-hangzhou.aliyuncs.com', Status: 'available' }] } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Alibaba Cloud fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-alibaba-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new AlibabaClient({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ accessKeyId: new SecretString('test-access-key-id'), accessKeySecret: new SecretString('test-access-key-secret') }) });
  const flow = new ECSRegionsFlow(newConnection(provider, { provider: 'alibaba', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `alibaba-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<DescribeRegionsOutput>()).toEqual({ regions: [{ regionId: 'cn-hangzhou', localName: '华东1（杭州）', endpoint: 'ecs.cn-hangzhou.aliyuncs.com', status: 'available' }] });
    expect(requests).toBe(2);
    expect(nonces.size).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
