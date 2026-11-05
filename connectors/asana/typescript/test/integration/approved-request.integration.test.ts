// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client as DexClient, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { Client as AsanaClient, newConnection } from '../../src/index.js';
import { ApprovedRequestFlow, ConnectionName, type ApprovedRequestResult } from '../../examples/approved-request-task/flow.js';

it('runs the checked-in Asana Flow through a real Dex Worker, retry, and optional branch failure', async () => {
  const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!serverAddress) throw new TypeError('DEX_FLOW_SERVICE_ADDRESS must name a running dexcli dev service');
  const taskId = '1204567890123456';
  const projectId = '1201000000000001';
  const storyId = '1205000000000001';
  const requests: string[] = [];
  let firstList = true;
  const provider = createServer(async (request, response) => {
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    const body = Buffer.concat(chunks).toString('utf8');
    const path = new URL(request.url ?? '/', 'http://localhost').pathname;
    requests.push(`${request.method} ${path}`);
    expect(request.headers.authorization).toBe('Bearer inert-asana-token');
    response.setHeader('Content-Type', 'application/json');
    if (path === '/api/1.0/tasks' && request.method === 'GET') {
      if (firstList) { firstList = false; response.writeHead(429, { 'Retry-After': '1' }).end('{}'); return; }
      response.end(JSON.stringify({ data: [] })); return;
    }
    if (path === '/api/1.0/tasks' && request.method === 'POST') {
      if (JSON.parse(body).data.name === 'blocked') { response.writeHead(400).end('{}'); return; }
      response.writeHead(201).end(JSON.stringify({ data: { gid: taskId, name: 'Approved access', created_at: '2026-09-30T16:15:00Z' } })); return;
    }
    if (path === `/api/1.0/tasks/${taskId}` && (request.method === 'GET' || request.method === 'PUT')) {
      response.end(JSON.stringify({ data: { gid: taskId, name: 'Approved access', completed: request.method === 'PUT', created_at: '2026-09-30T16:15:00Z', modified_at: '2026-09-30T16:20:00Z' } })); return;
    }
    if (path === `/api/1.0/tasks/${taskId}/stories` && request.method === 'POST') {
      expect(JSON.parse(body).data.text).toBe('Approved by Ada.');
      response.writeHead(201).end(JSON.stringify({ data: { gid: storyId, created_at: '2026-09-30T16:25:00Z' } })); return;
    }
    response.writeHead(404).end('{}');
  });
  let providerPort = 0;
  for (let port = 21160; port <= 21179; port++) {
    try {
      provider.listen(port, '127.0.0.1');
      await once(provider, 'listening');
      providerPort = port;
      break;
    } catch { /* Try the next reserved local port. */ }
  }
  if (!providerPort) throw new Error('no provider test port available in 21160-21179');
  const directory = await mkdtemp(join(tmpdir(), 'dex-asana-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const asana = new AsanaClient({ endpoint: `http://127.0.0.1:${providerPort}/api/1.0` }, {
    resolve: async () => ({ accessToken: new SecretString('inert-asana-token') }),
  });
  const flow = new ApprovedRequestFlow(newConnection(asana, { provider: 'asana', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new DexClient(registry, cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const firstId = `asana-approved-${randomUUID()}`;
    await client.startFlow(flow, firstId, { projectId, name: 'Approved access', comment: 'Approved by Ada.' });
    const first = await client.waitForFlow(firstId, 60_000);
    expect(first.status, first.errorMessage).toBe('completed');
    expect(first.singleOutput<ApprovedRequestResult>()).toEqual({ taskId, commentId: storyId });
    expect(requests).toEqual([
      'GET /api/1.0/tasks', 'GET /api/1.0/tasks', 'POST /api/1.0/tasks',
      `GET /api/1.0/tasks/${taskId}`, `PUT /api/1.0/tasks/${taskId}`, `POST /api/1.0/tasks/${taskId}/stories`,
    ]);
    const rejectedId = `asana-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { projectId, name: 'blocked', comment: 'Approved by Ada.' });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => provider.close(() => resolve()));
  }
}, 90_000);
