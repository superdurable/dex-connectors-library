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
import { LinearClient } from '../../src/client.js';
import { newConnection, type SearchIssuesOutput } from '../../src/index.js';
import { ConnectionName, IssueSearchFlow } from '../../examples/issue-search/flow/workflow.js';

it('searches through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    const chunks: Buffer[] = [];
    request.on('data', chunk => chunks.push(Buffer.from(chunk)));
    request.on('end', () => {
      requests.push(Buffer.concat(chunks).toString());
      if (request.headers.authorization !== 'linear-integration-key') { response.writeHead(401).end('{}'); return; }
      if (requests.length === 2) { response.writeHead(403).end('{"errors":[{"extensions":{"code":"FORBIDDEN"}}]}'); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ data: { issues: { nodes: [{ id: '9e5f6a7b-8c9d-4e0f-8a1b-3c4d5e6f7a8b', identifier: 'ENG-7', title: 'Fire panel wiring', url: 'https://linear.app/acme/issue/ENG-7', priority: 2, createdAt: '2026-01-28T10:00:00Z', updatedAt: '2026-01-28T10:05:00Z', team: { id: '2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4', key: 'ENG', name: 'Engineering' }, state: { id: '5a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d', name: 'Todo', type: 'unstarted' } }], pageInfo: { hasNextPage: false, endCursor: null } } } }));
    });
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Linear stand-in port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-linear-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'linear', name: ConnectionName };
  const credentials = new StaticCredentialProvider([[connection, { authMethodId: 'personal-api-key', apiKey: new SecretString('linear-integration-key') }]]);
  const provider = new LinearClient({}, credentials, { endpoint: `http://127.0.0.1:${address.port}/graphql` });
  const flow = new IssueSearchFlow(newConnection(provider, connection));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `linear-search-${randomUUID()}`;
    await client.startFlow(flow, successId, { filter: {}, pageSize: 1 });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<SearchIssuesOutput>().issues?.[0]?.identifier).toBe('ENG-7');
    const rejectedId = `linear-denied-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { filter: {}, pageSize: 1 });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
