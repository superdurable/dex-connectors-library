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
import { HootsuiteClient } from '../../src/client.js';
import { newConnection, type SocialProfilesPage } from '../../src/index.js';
import { ConnectionName, SocialProfilesFlow } from '../../examples/social-profiles/flow.js';

it('retries a provider 503 and persists social profiles through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer synthetic-hootsuite-token') { response.writeHead(401).end(); return; }
    if (request.url !== '/v1/socialProfiles') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ data: [{ id: '115185509', type: 'FACEBOOK', socialNetworkUsername: 'Example Page' }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Hootsuite fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-hootsuite-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new HootsuiteClient({ baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => ({ oauthClientId: 'synthetic-client', oauthClientSecret: new SecretString('synthetic-secret'), accessToken: new SecretString('synthetic-hootsuite-token'), refreshToken: new SecretString('synthetic-refresh') }),
  });
  const connection = newConnection(provider, { provider: 'hootsuite', name: ConnectionName });
  const flow = new SocialProfilesFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `hootsuite-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<SocialProfilesPage>().profiles?.[0]?.id).toBe('115185509');
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
