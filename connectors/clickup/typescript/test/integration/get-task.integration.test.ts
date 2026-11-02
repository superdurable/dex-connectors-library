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
import { ClickUpClient } from '../../src/client.js';
import { newConnection, type Task } from '../../src/index.js';
import { ConnectionName, GetTaskFlow } from '../../examples/get-task/flow/workflow.js';

it('reads a task through real Dex and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'pk_4411723_TESTTOKEN0123456789ABCDEFGHIJ') { response.writeHead(401).end(); return; }
    if (request.url?.includes('86b2denied')) { response.writeHead(403, { 'Content-Type': 'application/json' }).end('{"ECODE":"OAUTH_019"}'); return; }
    if (request.method !== 'GET' || !request.url?.startsWith('/api/v2/task/86b2x4k7q?')) { response.writeHead(404).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ id: '86b2x4k7q', name: 'Fix login', status: { status: 'open', type: 'open' }, assignees: [], tags: [] }));
  });
  server.listen(21178, '127.0.0.1');
  await once(server, 'listening');
  const directory = await mkdtemp(join(tmpdir(), 'dex-clickup-get-task-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new ClickUpClient({}, { resolve: async () => ({ apiToken: new SecretString('pk_4411723_TESTTOKEN0123456789ABCDEFGHIJ') }) },
    { apiBaseURL: 'http://127.0.0.1:21178/api/v2' });
  const flow = new GetTaskFlow(newConnection(provider, { provider: 'clickup', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `clickup-get-task-${randomUUID()}`;
    await client.startFlow(flow, successId, { taskId: '86b2x4k7q' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<Task>().id).toBe('86b2x4k7q');
    const rejectionId = `clickup-get-task-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { taskId: '86b2denied' });
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
