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
import { BilibiliClient, newConnection, type AuthorizedUser } from '../../src/index.js';
import { BilibiliAuthorizedUserFlow, ConnectionName, authorizedUserAttribute } from '../../examples/authorized-user/flow.js';

const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801';
let requests = 0;
let isRejecting = false;
const provider = createServer((request, response) => {
  expect(request.method).toBe('GET');
  expect(request.url).toBe('/arcopen/fn/user/account/info');
  expect(request.headers['access-token']).toBe('token789');
  expect(request.headers['x-bili-signature-nonce']).toBe('00000000000000000000000000000000');
  expect(request.headers['x-bili-timestamp']).toBe('1730185575');
  expect(request.headers.authorization).toMatch(/^[0-9a-f]{64}$/);
  requests++;
  if (isRejecting) { response.writeHead(200).end(JSON.stringify({ code: 123456 })); return; }
  if (requests === 1) { response.writeHead(503).end(); return; }
  response.writeHead(200).end(JSON.stringify({ code: 0, data: { openid: 'public-user-id', name: 'sample_creator' } }));
});
afterAll(() => provider.close());

it('runs the checked-in Flow through Dex, retries, and persists only public identity', async () => {
  provider.listen(0, '127.0.0.1');
  await once(provider, 'listening');
  const address = provider.address();
  if (!address || typeof address === 'string') throw new TypeError('local provider address unavailable');
  const reference = { provider: 'bilibili', name: ConnectionName };
  const credentials = new StaticCredentialProvider([[reference, {
    clientID: 'client123', appSecret: new SecretString('secret456'), accessToken: new SecretString('token789'),
  }]]);
  const connector = new BilibiliClient({ baseUrl: `http://127.0.0.1:${address.port}` }, credentials, {
    now: () => new Date(1_730_185_575_000), nonce: () => new Uint8Array(16),
  });
  const flow = new BilibiliAuthorizedUserFlow(newConnection(connector, reference));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-bilibili-ts-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `bilibili-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<AuthorizedUser>()).toEqual({ openID: 'public-user-id', name: 'sample_creator' });
    expect(await client.getAttributes(flowId, authorizedUserAttribute)).toEqual([{ openID: 'public-user-id', name: 'sample_creator' }]);
    expect(requests).toBe(2);
    isRejecting = true;
    const rejectedId = `bilibili-ts-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, {});
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toMatch(/providerRejected/);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
