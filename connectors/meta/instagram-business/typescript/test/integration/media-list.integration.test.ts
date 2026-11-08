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
import { expect, it } from 'vitest';
import { InstagramBusinessClient } from '../../src/client.js';
import { newConnection, type MediaListPage } from '../../src/index.js';
import { ConnectionName, MediaListFlow } from '../../examples/media-list/flow.js';

it('retries a provider 503 and persists the media page through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer synthetic-instagram-token') { response.writeHead(401).end(); return; }
    if (request.url !== '/v25.0/17900000000000001/media?fields=id%2Ccaption%2Cmedia_type%2Ctimestamp%2Cpermalink&limit=100&after=cursor') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ data: [{ id: '17900000000000001', caption: 'Hello', media_type: 'IMAGE', timestamp: '2026-11-08T10:00:00+0000', permalink: 'https://www.instagram.com/p/example/' }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Instagram Business fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-instagram-business-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new InstagramBusinessClient({ baseUrl: `http://127.0.0.1:${address.port}/v25.0` }, {
    resolve: async () => ({ userAccessToken: new SecretString('synthetic-instagram-token') }),
  });
  const connection = newConnection(provider, { provider: 'instagram-business', name: ConnectionName });
  const flow = new MediaListFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `instagram-business-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { igUserId: '17900000000000001', after: 'cursor' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<MediaListPage>().media?.[0]?.id).toBe('17900000000000001');
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
