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
import { SlackClient } from '../../src/client.js';
import { newConnection, type ListThreadMessagesOutput } from '../../src/index.js';
import { ConnectionName, ListThreadMessagesFlow } from '../../examples/list-thread-messages/flow/workflow.js';

it('reads a Slack page through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.method !== 'GET' || !request.url?.startsWith('/api/conversations.replies?')) { response.writeHead(404).end('{}'); return; }
    if (request.headers.authorization !== 'Bearer user-token') { response.writeHead(401).end('{"ok":false,"error":"invalid_auth"}'); return; }
    if (request.url.includes('cursor=reject')) { response.writeHead(403).end('{"ok":false,"error":"no_permission"}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end('{"ok":true,"messages":[{"ts":"1.0","user":"U1","text":"request"},{"ts":"2.0","thread_ts":"1.0","user":"U2","text":"approved"}]}');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Slack fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-slack-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new SlackClient({ endpoint: `http://127.0.0.1:${address.port}/api` }, {
    resolve: async () => ({ botToken: new SecretString('bot-token'), userToken: new SecretString('user-token') }),
    resolveWithRefresh: async () => ({ botToken: new SecretString('bot-token'), userToken: new SecretString('user-token') }),
  });
  const flow = new ListThreadMessagesFlow(newConnection(provider, { provider: 'slack', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const input = { channelId: 'C123', threadTimestamp: '1.0', pageSize: 15 };
    const successId = `slack-thread-${randomUUID()}`;
    await client.startFlow(flow, successId, input);
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<ListThreadMessagesOutput>()?.messages?.[1]?.text).toBe('approved');
    const rejectionId = `slack-thread-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { ...input, cursor: 'reject' });
    const rejected = await client.waitForFlow(rejectionId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
