// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { GitHubClient } from '../../src/client.js';
import { newConnection, type ReleasePage } from '../../src/index.js';
import { ConnectionName, RepositoryReleasesFlow } from '../../examples/repository-releases/flow/workflow.js';

it('reads releases through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer one-use-token') { response.writeHead(401).end(); return; }
    if (/[?&]page=2(?:&|$)/.test(request.url ?? '')) { response.writeHead(403).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json', 'X-OAuth-Scopes': 'read:user, user:email' });
    response.end(JSON.stringify([{ id: 100, tag_name: 'v1.0.0', name: 'Initial release', html_url: 'https://github.com/octocat/hello-world/releases/tag/v1.0.0', body: 'Ready', draft: false, prerelease: false, created_at: '2026-02-01T00:00:00Z', published_at: '2026-02-02T00:00:00Z' }]));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('GitHub stand-in port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-github-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'github', name: ConnectionName };
  const credentials = new StaticCredentialProvider([[connection, { accessToken: new SecretString('one-use-token'), oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret') }]]);
  const provider = new GitHubClient({ baseUrl: `http://127.0.0.1:${address.port}` }, credentials);
  const flow = new RepositoryReleasesFlow(newConnection(provider, connection));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `github-releases-${randomUUID()}`;
    await client.startFlow(flow, successId, { owner: 'octocat', repository: 'hello-world', pageSize: 2, page: 1 });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    const page = success.singleOutput<ReleasePage>();
    expect(page.releases?.[0]?.tagName).toBe('v1.0.0');
    const rejectedId = `github-denied-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { owner: 'octocat', repository: 'hello-world', pageSize: 2, page: 2 });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('insufficientScope');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
