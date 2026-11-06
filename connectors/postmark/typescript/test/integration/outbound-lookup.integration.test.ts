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
import { PostmarkClient } from '../../src/client.js';
import { newConnection, type MessagePage } from '../../src/index.js';
import { ConnectionName, OutboundLookupFlow } from '../../examples/outbound-lookup/flow.js';

it('runs the Postmark example through a real Dex Worker and Client', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  let retryCount = 0;
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers['x-postmark-server-token'] !== 'test-secret-token') { response.writeHead(401).end(); return; }
    if (request.url?.includes('count=26')) { response.writeHead(403).end(); return; }
    if (request.url?.includes('count=27') && retryCount++ === 0) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end('{"TotalCount":1,"Messages":[{"MessageID":"msg-1","Subject":"Welcome","Status":"Sent"}]}');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('provider fixture port is unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-postmark-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new PostmarkClient({ baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => ({ apiKey: new SecretString('test-secret-token') }),
  });
  const flow = new OutboundLookupFlow(newConnection(provider, { provider: 'postmark', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successfulID = `postmark-list-${randomUUID()}`;
    await client.startFlow(flow, successfulID, { count: 25, offset: 0 });
    const success = await client.waitForFlow(successfulID, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<MessagePage>().Messages?.[0]?.MessageID).toBe('msg-1');

    const rejectedID = `postmark-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedID, { count: 26, offset: 0 });
    const rejected = await client.waitForFlow(rejectedID, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');

    const retryID = `postmark-retry-${randomUUID()}`;
    await client.startFlow(flow, retryID, { count: 27, offset: 0 });
    const retried = await client.waitForFlow(retryID, 60_000);
    expect(retried.status, retried.errorMessage).toBe('completed');
    expect(requests.filter(request => request.includes('count=27'))).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
