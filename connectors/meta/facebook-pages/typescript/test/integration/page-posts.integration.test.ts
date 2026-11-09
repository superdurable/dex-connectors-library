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
import { FacebookPagesClient } from '../../src/client.js';
import { newConnection, type PagePostsPage } from '../../src/index.js';
import { ConnectionName, PagePostsFlow } from '../../examples/page-posts/flow.js';

it('retries a provider 503 and persists the post page through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer synthetic-page-token') { response.writeHead(401).end(); return; }
    if (request.url !== '/v25.0/123456789/feed?fields=id%2Cmessage%2Ccreated_time%2Cpermalink_url&limit=100&after=cursor') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ data: [{ id: '123456789_987654321', message: 'Welcome', created_time: '2026-11-08T10:00:00+0000', permalink_url: 'https://www.facebook.com/123456789/posts/987654321' }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Facebook Pages fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-facebook-pages-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new FacebookPagesClient({ baseUrl: `http://127.0.0.1:${address.port}/v25.0` }, {
    resolve: async () => ({ pageAccessToken: new SecretString('synthetic-page-token') }),
  });
  const connection = newConnection(provider, { provider: 'facebook-pages', name: ConnectionName });
  const flow = new PagePostsFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `facebook-pages-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { pageId: '123456789', after: 'cursor' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<PagePostsPage>().posts?.[0]?.id).toBe('123456789_987654321');
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
