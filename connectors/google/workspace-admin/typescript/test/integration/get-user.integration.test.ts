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
import { WorkspaceAdminClient } from '../../src/client.js';
import { newConnection, type User } from '../../src/index.js';
import { ConnectionName, GetWorkspaceUserFlow } from '../../examples/get-user/flow/workflow.js';

it('reads an account through a real Dex Worker and fails an unwired notFound branch', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer test-directory-token') { response.writeHead(401).end(); return; }
    if (request.url?.endsWith('/missing@example.com')) { response.writeHead(404).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ id: '1001', primaryEmail: 'ada@example.com', name: { givenName: 'Ada', familyName: 'Lovelace' }, orgUnitPath: '/Engineering', suspended: false }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Directory stand-in port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-workspace-admin-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const reference = { provider: 'google', name: ConnectionName };
  const provider = new WorkspaceAdminClient({ endpoint: `http://127.0.0.1:${address.port}` }, new StaticCredentialProvider([[reference, { accessToken: new SecretString('test-directory-token') }]]));
  const flow = new GetWorkspaceUserFlow(newConnection(provider, reference));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `workspace-user-${randomUUID()}`;
    await client.startFlow(flow, successId, { userKey: 'ada@example.com' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    const user = success.singleOutput<User>();
    expect(user.id).toBe('1001');
    expect(user.name.givenName).toBe('Ada');
    const missingId = `workspace-user-missing-${randomUUID()}`;
    await client.startFlow(flow, missingId, { userKey: 'missing@example.com' });
    const missing = await client.waitForFlow(missingId, 60_000);
    expect(missing.status).toBe('failed');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
