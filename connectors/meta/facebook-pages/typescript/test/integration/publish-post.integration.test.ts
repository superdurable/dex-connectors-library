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
import { PublishPostFlow, ConnectionName } from '../../examples/publish-post/flow.js';
import { FacebookPagesClient } from '../../src/client.js';
import { newConnection, type PublishedPost } from '../../src/index.js';

it('retries a safe 503 and fails an uncertain publish without resending through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const messages: string[] = [];
  const server = createServer(async (request, response) => {
    if (request.headers.authorization !== 'Bearer synthetic-page-token' || request.url !== '/v25.0/123456789/feed' || request.method !== 'POST' || request.headers['content-type'] !== 'application/x-www-form-urlencoded') { response.writeHead(400).end(); return; }
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    const message = new URLSearchParams(Buffer.concat(chunks).toString()).get('message') ?? '';
    messages.push(message);
    if (message === 'Maybe published') { response.writeHead(500).end(); return; }
    if (message !== 'Hello from Dex') { response.writeHead(400).end(); return; }
    if (messages.length === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ id: '123456789_987654321' }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Facebook Pages fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-facebook-pages-ts-publish-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new FacebookPagesClient({ baseUrl: `http://127.0.0.1:${address.port}/v25.0` }, {
    resolve: async () => ({ pageAccessToken: new SecretString('synthetic-page-token') }),
  });
  const connection = newConnection(provider, { provider: 'facebook-pages', name: ConnectionName });
  const flow = new PublishPostFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const publishedFlowId = `facebook-pages-ts-publish-${randomUUID()}`;
    await client.startFlow(flow, publishedFlowId, { pageId: '123456789', message: 'Hello from Dex' });
    const published = await client.waitForFlow(publishedFlowId, 60_000);
    expect(published.status, published.errorMessage).toBe('completed');
    expect(published.singleOutput<PublishedPost>().postId).toBe('123456789_987654321');
    expect(messages).toEqual(['Hello from Dex', 'Hello from Dex']);
    const uncertainFlowId = `facebook-pages-ts-uncertain-${randomUUID()}`;
    await client.startFlow(flow, uncertainFlowId, { pageId: '123456789', message: 'Maybe published' });
    const uncertain = await client.waitForFlow(uncertainFlowId, 60_000);
    expect(uncertain.status).toBe('failed');
    expect(uncertain.errorMessage).toContain('uncertain');
    expect(messages).toEqual(['Hello from Dex', 'Hello from Dex', 'Maybe published']);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
