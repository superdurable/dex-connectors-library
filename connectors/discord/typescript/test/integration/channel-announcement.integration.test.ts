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
import { DiscordClient } from '../../src/client.js';
import { newConnection, type Message } from '../../src/index.js';
import { ChannelAnnouncementFlow, ConnectionName } from '../../examples/channel-announcement/flow/workflow.js';

it('creates a message through real Dex and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.method !== 'POST' || request.url !== '/api/v10/channels/100000000000000001/messages') { response.writeHead(404).end(); return; }
    if (request.headers.authorization !== 'Bot MTAxTESTbot.token-part.0123456789abcdef') { response.writeHead(401).end(); return; }
    const chunks: Buffer[] = [];
    request.on('data', (chunk: Buffer) => chunks.push(chunk));
    request.on('end', () => {
      const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { content: string; nonce: string; enforce_nonce: boolean; allowed_mentions: { parse: string[] } };
      if (!body.enforce_nonce || !/^[A-Za-z0-9_-]{25}$/.test(body.nonce) || body.allowed_mentions.parse.length !== 0) { response.writeHead(400).end(); return; }
      if (body.content === 'REJECT') { response.writeHead(403, { 'Content-Type': 'application/json' }).end('{"code":50013}'); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ id: '300000000000000001', channel_id: '100000000000000001', author: { id: '400000000000000001', username: 'releasebot', bot: true }, content: body.content, timestamp: '2026-10-26T12:00:00Z', type: 0 }));
    });
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Discord fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-discord-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new DiscordClient({ endpoint: `http://127.0.0.1:${address.port}/api/v10` }, {
    resolve: async () => ({ botToken: new SecretString('MTAxTESTbot.token-part.0123456789abcdef') }),
  });
  const flow = new ChannelAnnouncementFlow(newConnection(provider, { provider: 'discord', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `discord-announcement-${randomUUID()}`;
    await client.startFlow(flow, successId, { channelId: '100000000000000001', content: 'Release today @everyone' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<Message>().content).toBe('Release today @everyone');
    const rejectionId = `discord-announcement-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { channelId: '100000000000000001', content: 'REJECT' });
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
