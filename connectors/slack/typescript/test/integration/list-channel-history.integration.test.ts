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
import { newConnection, type ListChannelMessagesOutput } from '../../src/index.js';
import { ConnectionName, ListChannelMessagesFlow } from '../../examples/list-channel-messages/flow/workflow.js';

it('reads a Slack channel history page through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.method !== 'GET' || !request.url?.startsWith('/api/conversations.history?')) { response.writeHead(404).end('{}'); return; }
    if (request.headers.authorization !== 'Bearer bot-token') { response.writeHead(401).end('{"ok":false,"error":"invalid_auth"}'); return; }
    if (request.url.includes('channel=C404')) { response.writeHead(200).end('{"ok":false,"error":"not_in_channel"}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end('{"ok":true,"messages":[{"ts":"2.0","user":"U2","text":"newer"},{"ts":"1.0","thread_ts":"1.0","reply_count":1,"user":"UBOT","bot_id":"B1","text":"ref:order-1042"}],"has_more":true,"response_metadata":{"next_cursor":"older"}}');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Slack fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-slack-channel-history-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new SlackClient({ endpoint: `http://127.0.0.1:${address.port}/api` }, {
    resolve: async () => ({ botToken: new SecretString('bot-token'), userToken: new SecretString('user-token') }),
    resolveWithRefresh: async () => ({ botToken: new SecretString('bot-token'), userToken: new SecretString('user-token') }),
  });
  const flow = new ListChannelMessagesFlow(newConnection(provider, { provider: 'slack', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `slack-channel-history-${randomUUID()}`;
    await client.startFlow(flow, successId, { channelId: 'C123', oldestTimestamp: '0.5' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<ListChannelMessagesOutput>()).toEqual({
      messages: [
        { channelId: 'C123', timestamp: '2.0', userId: 'U2', text: 'newer' },
        { channelId: 'C123', timestamp: '1.0', threadTimestamp: '1.0', userId: 'UBOT', botId: 'B1', text: 'ref:order-1042', replyCount: 1 },
      ],
      nextCursor: 'older',
    });
    expect(requests[0]).toBe('GET /api/conversations.history?channel=C123&limit=15&oldest=0.5');
    const rejectionId = `slack-channel-history-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { channelId: 'C404' });
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
