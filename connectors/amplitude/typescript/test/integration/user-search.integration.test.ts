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
import { AmplitudeClient } from '../../src/client.js';
import { newConnection, type UserSearchPage, type Credentials } from '../../src/index.js';
import { ConnectionName, UserSearchFlow } from '../../examples/user-search/flow.js';

it('retries a provider 503 and persists the worker through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const user = 'example-user';
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== `Basic ${Buffer.from('sample-amplitude-api-key:sample-amplitude-secret-key').toString('base64')}`) { response.writeHead(401).end(); return; }
    if (!request.url || new URL(request.url, 'http://localhost').pathname !== '/api/2/usersearch' || new URL(request.url, 'http://localhost').searchParams.get('user') !== user) { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
      matches: [{ user_id: 'example-user', amplitude_id: 12345 }], type: 'match_user_or_device_id',
    }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Amplitude fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-amplitude-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { apiKey: new SecretString('sample-amplitude-api-key'), secretKey: new SecretString('sample-amplitude-secret-key') };
  const provider = new AmplitudeClient({ baseUrl: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => credentials,
  });
  const connection = newConnection(provider, { provider: 'amplitude', name: ConnectionName });
  const flow = new UserSearchFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `amplitude-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { user });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<UserSearchPage>().user).toBe(user);
    expect(result.singleOutput<UserSearchPage>().userIDs).toEqual(['example-user']);
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
