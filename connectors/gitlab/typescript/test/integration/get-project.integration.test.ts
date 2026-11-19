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
import { GitLabClient } from '../../src/client.js';
import { newConnection, type Project } from '../../src/index.js';
import { ConnectionName, GetProjectFlow } from '../../examples/get-project/flow/workflow.js';

it('reads a project through real Dex and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers['private-token'] !== 'test-accesstoken') { response.writeHead(401).end(); return; }
    if (request.url === '/projects/404') { response.writeHead(404).end(); return; }
    if (request.method !== 'GET' || request.url !== '/projects/42') { response.writeHead(400).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ id: 42, name: 'Meridian Web', path_with_namespace: 'meridian/web', web_url: 'https://gitlab.com/meridian/web' }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('GitLab fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-gitlab-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new GitLabClient({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ accessToken: new SecretString('test-accesstoken') }) });
  const flow = new GetProjectFlow(newConnection(provider, { provider: 'gitlab', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `gitlab-project-${randomUUID()}`;
    await client.startFlow(flow, successId, { projectId: '42' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<Project>().id).toBe(42);
    const rejectionId = `gitlab-project-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { projectId: '404' });
    const rejected = await client.waitForFlow(rejectionId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('notFound');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
