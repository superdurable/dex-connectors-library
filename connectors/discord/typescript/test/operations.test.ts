// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { createServer } from 'node:http';
import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { DiscordClient } from '../src/client.js';

const call = { id: 'deb1de08-84b3-554e-91d5-e46e685d1f56', idempotencyKey: 'test-key',
  connection: { provider: 'discord', name: 'discord' }, operation: { connectorId: 'discord', operationId: 'addReaction' },
  context: { cancellationSignal: new AbortController().signal } } as Call;
const channelId = '100000000000000001';
const messageId = '300000000000000001';
const credential = { resolve: async () => ({ botToken: new SecretString('MTAxTESTbot.token-part.0123456789abcdef') }) };

it('treats revoked credentials as a terminal rejection without HTTP', async () => {
  let requestCount = 0;
  const client = new DiscordClient({}, { resolve: async () => { throw new ReauthorizationRequiredError(); } }, {
    send: async () => { requestCount++; throw new Error('unexpected request'); },
  });
  const result = await client.getChannel().invoke(call, { channelId });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') { expect(result.branch).toBe('providerRejected'); expect(result.failure?.kind).toBe('AUTHENTICATION'); }
  expect(requestCount).toBe(0);
});

it('uses Go PathEscape for Unicode reactions and retries a refused local connection', async () => {
  let path = '';
  const scripted = new DiscordClient({}, credential, { send: async request => {
    path = new URL(request.url).pathname;
    return { status: 204, headers: {}, body: Buffer.alloc(0) };
  } });
  const success = await scripted.addReaction().invoke(call, { channelId, messageId, emoji: '📣' });
  expect(success.kind).toBe('branch');
  expect(path).toBe(`/api/v10/channels/${channelId}/messages/${messageId}/reactions/%F0%9F%93%A3/@me`);

  const server = createServer();
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('fixture port unavailable');
  await new Promise<void>(resolve => server.close(() => resolve()));
  const refused = new DiscordClient({ endpoint: `http://127.0.0.1:${address.port}` }, credential);
  const attempt = await refused.getChannel().invoke(call, { channelId });
  expect(attempt.kind).toBe('retry');
  if (attempt.kind === 'retry') expect(attempt.failure.kind).toBe('TRANSPORT');
});

it('classifies an oversized 403 body from status without retaining provider text', async () => {
  const server = createServer((_request, response) => { response.writeHead(403).end('secret provider text '.repeat(10_000)); });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('fixture port unavailable');
  try {
    const client = new DiscordClient({ endpoint: `http://127.0.0.1:${address.port}` }, credential);
    const attempt = await client.getChannel().invoke(call, { channelId });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind === 'branch') { expect(attempt.branch).toBe('providerRejected'); expect(attempt.failure?.kind).toBe('AUTHORIZATION'); expect(attempt.failure?.message).not.toContain('secret provider text'); }
  } finally { await new Promise<void>(resolve => server.close(() => resolve())); }
});
