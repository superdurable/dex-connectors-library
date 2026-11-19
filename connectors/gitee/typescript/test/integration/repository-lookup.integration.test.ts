// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterAll, expect, it } from 'vitest';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { GiteeClient, newConnection, type GetRepositoryOutput } from '../../src/index.js';
import { GiteeRepositoryLookupFlow, ConnectionName, repositoryAttribute } from '../../examples/repository-lookup/flow.js';

const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801';
let requests = 0;
const provider = createServer((request, response) => {
  const url = new URL(request.url ?? '', 'http://127.0.0.1');
  if (request.method !== 'GET' || url.searchParams.get('access_token') !== 'test-gitee-token' || request.headers.accept !== 'application/json') {
    response.writeHead(401).end(); return;
  }
  requests++;
  if (url.pathname.endsWith('/missing')) { response.writeHead(404).end('{}'); return; }
  if (url.pathname !== '/api/v5/repos/open-source/demo') { response.writeHead(400).end(); return; }
  if (requests === 1) { response.writeHead(503).end(); return; }
  response.writeHead(200).end(JSON.stringify({
    id: 101, full_name: 'open-source/demo', name: 'demo', description: 'Example repository',
    html_url: 'https://gitee.com/open-source/demo', private: false, default_branch: 'master',
    stargazers_count: 5, forks_count: 2,
  }));
});
afterAll(() => provider.close());

it('retries the checked-in Flow, persists metadata, and fails on an unwired rejection', async () => {
  provider.listen(0, '127.0.0.1'); await once(provider, 'listening');
  const address = provider.address();
  if (!address || typeof address === 'string') throw new TypeError('local provider address unavailable');
  const reference = { provider: 'gitee', name: ConnectionName };
  const credentials = new StaticCredentialProvider([[reference, { accessToken: new SecretString('test-gitee-token') }]]);
  const connector = new GiteeClient({ baseUrl: 'http://127.0.0.1:' + address.port }, credentials);
  const flow = new GiteeRepositoryLookupFlow(newConnection(connector, reference));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-gitee-ts-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = 'gitee-ts-' + randomUUID();
    await client.startFlow(flow, flowId, { owner: 'open-source', repo: 'demo' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const output: GetRepositoryOutput = { repository: {
      id: 101, fullName: 'open-source/demo', name: 'demo', description: 'Example repository',
      htmlUrl: 'https://gitee.com/open-source/demo', private: false, defaultBranch: 'master', starsCount: 5, forksCount: 2,
    } };
    expect(result.singleOutput<GetRepositoryOutput>()).toEqual(output);
    expect(await client.getAttributes(flowId, repositoryAttribute)).toEqual([output]);
    expect(requests).toBe(2);
    const rejectedId = 'gitee-ts-rejected-' + randomUUID();
    await client.startFlow(flow, rejectedId, { owner: 'open-source', repo: 'missing' });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toMatch(/notFound/);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
