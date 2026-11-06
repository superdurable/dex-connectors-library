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
import { LinkedInClient } from '../../src/client.js';
import { newConnection, type AuthenticatedProfile } from '../../src/index.js';
import { ConnectionName, AuthenticatedProfileFlow } from '../../examples/authenticated-profile/flow/workflow.js';



it('lists a provider page through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer one-use-token') { response.writeHead(401).end(); return; }
    if (requests.length === 2) { response.writeHead(403).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ sub: 'member-1', name: 'Ada', email: 'ada@example.com', email_verified: true }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('LinkedIn stand-in port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-linkedin-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'linkedin', name: ConnectionName };
  const provider = new LinkedInClient({ userInfoUrl: `http://127.0.0.1:${address.port}/v2/userinfo` }, new StaticCredentialProvider([[connection, { accessToken: new SecretString('one-use-token') }]]));
  const flow = new AuthenticatedProfileFlow(newConnection(provider, connection));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `linkedin-list-${randomUUID()}`;
    await client.startFlow(flow, successId, {});
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    const profile = success.singleOutput<AuthenticatedProfile>();
    expect(profile.verifiedEmail).toBe('ada@example.com');
    const rejectedId = `linkedin-denied-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, {});
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
