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
import { SentryClient } from '../../src/client.js';
import { newConnection, type IssuePage } from '../../src/index.js';
import { ConnectionName, IssueReviewFlow } from '../../examples/issue-review/flow/workflow.js';

it('lists an issue page through real Dex and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.method !== 'GET' || !request.url?.startsWith('/api/0/organizations/acme/issues/')) { response.writeHead(404).end(); return; }
    if (request.headers.authorization !== 'Bearer test-secret-token') { response.writeHead(401).end(); return; }
    if (request.url.includes('REJECT')) { response.writeHead(403).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end('[{"id":"123","shortId":"APP-1","title":"ExampleError","status":"unresolved"}]');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Sentry fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-sentry-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new SentryClient({ baseUrl: `http://127.0.0.1:${address.port}/api/0` }, {
    resolve: async () => ({ apiKey: new SecretString('test-secret-token') }),
  });
  const flow = new IssueReviewFlow(newConnection(provider, { provider: 'sentry', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `sentry-list-${randomUUID()}`;
    await client.startFlow(flow, successId, { organization: 'acme', query: 'is:unresolved' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<IssuePage>().issues?.[0]?.id).toBe('123');
    const rejectionId = `sentry-list-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { organization: 'acme', query: 'REJECT' });
    const rejected = await client.waitForFlow(rejectionId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
