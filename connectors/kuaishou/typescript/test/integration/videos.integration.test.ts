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
import { ConnectionName, UserVideosFlow } from '../../examples/videos/flow.js';
import { KuaishouClient } from '../../src/client.js';
import { newConnection, type UserVideosPage } from '../../src/index.js';

it('retries Kuaishou 503 through a real Dex Worker and persists the observation', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'GET' || request.url !== '/openapi/photo/list?app_id=app123&access_token=token456&count=20' || request.headers.accept !== 'application/json') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ result: 1, video_list: [{ photo_id: 'photo-123', caption: 'Demo', create_time: 1596629598574, like_count: 1, comment_count: 2, view_count: 3, pending: false }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Kuaishou fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-kuaishou-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new KuaishouClient({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ accessToken: new SecretString('token456'), appID: 'app123' }) });
  const flow = new UserVideosFlow(newConnection(provider, { provider: 'kuaishou', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `kuaishou-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { cursor: '' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<UserVideosPage>()).toEqual({ videos: [{ photoID: 'photo-123', caption: 'Demo', createdAtMilliseconds: 1596629598574, likeCount: 1, commentCount: 2, viewCount: 3, pending: false }], nextCursor: 'photo-123' });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
