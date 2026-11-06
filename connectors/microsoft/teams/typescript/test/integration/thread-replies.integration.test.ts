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
import { TeamsClient } from '../../src/client.js';
import { newConnection, type ListThreadRepliesInput, type ListThreadRepliesOutput } from '../../src/index.js';
import { ConnectionName, ThreadRepliesFlow } from '../../examples/list-thread-replies/flow/workflow.js';

const team = 'fbe2bf47-16c8-47cf-b4a5-4b9b187c508b';
const channel = '19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2';
const root = '1616989510408';

it('reads a thread through real Dex and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer inert-teams-token') { response.writeHead(401).end(); return; }
    if (request.url?.includes('/1616989510412/replies')) {
      response.writeHead(404, { 'Content-Type': 'application/json' }).end('{"error":{"code":"NotFound"}}'); return;
    }
    if (requests.length === 1) { response.writeHead(429, { 'Retry-After': '1' }).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end('{"value":[]}');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Teams fixture has no TCP address');
  const directory = await mkdtemp(join(tmpdir(), 'dex-teams-replies-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new TeamsClient({ endpoint: `http://127.0.0.1:${address.port}/v1.0` }, {
    resolve: async () => ({
      oauthClientId: 'inert-client-id', oauthClientSecret: new SecretString('inert-secret'),
      accessToken: new SecretString('inert-teams-token'), refreshToken: new SecretString('inert-refresh-token'),
    }),
  });
  const flow = new ThreadRepliesFlow(newConnection(provider, { provider: 'microsoft', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const input: ListThreadRepliesInput = { teamId: team, channelId: channel, messageId: root };
    const successID = `teams-replies-${randomUUID()}`;
    await client.startFlow(flow, successID, input);
    const success = await client.waitForFlow(successID, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<ListThreadRepliesOutput>().replies).toEqual([]);
    const rejectedID = `teams-replies-${randomUUID()}`;
    await client.startFlow(flow, rejectedID, { teamId: team, channelId: channel, messageId: '1616989510412' });
    const rejected = await client.waitForFlow(rejectedID, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('notFound');
    expect(requests).toHaveLength(3);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
