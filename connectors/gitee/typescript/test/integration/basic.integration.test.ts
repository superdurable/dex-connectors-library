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
import { GiteeClient } from '../../src/client.js';
import { newConnection, type Credentials } from '../../src/index.js';
import { ConnectionName, GiteeInspectionFlow } from '../../examples/basic/flow.js';

it('retries a provider 503 and persists the result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((_request, response) => {
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ id: 101, full_name: 'open-source/demo', name: 'demo', description: 'Example repository', html_url: 'https://gitee.com/open-source/demo', private: false, default_branch: 'master', stargazers_count: 5, forks_count: 2 }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Gitee fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-gitee-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { accessToken: new SecretString('test-gitee-token') };
  const provider = new GiteeClient({}, { resolve: async () => credentials }, { send: async request => { const reply = await fetch(`http://127.0.0.1:${address.port}`, { method: request.method, headers: request.headers, body: request.body ? Buffer.from(request.body) : undefined }); return { status: reply.status, headers: Object.fromEntries(reply.headers), body: new Uint8Array(await reply.arrayBuffer()) }; } });
  const connection = newConnection(provider, { provider: 'gitee', name: ConnectionName });
  const flow = new GiteeInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `gitee-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { owner: 'open-source', repo: 'demo' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({ repository: { id: 101, fullName: 'open-source/demo', name: 'demo', description: 'Example repository', htmlUrl: 'https://gitee.com/open-source/demo', private: false, defaultBranch: 'master', starsCount: 5, forksCount: 2 } });
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
