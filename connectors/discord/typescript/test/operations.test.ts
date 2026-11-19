// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { createServer } from 'node:http';
import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { DiscordClient } from '../src/client.js';
import { CredentialMaterialError } from '../src/generated/models.js';

const call = { id: 'deb1de08-84b3-554e-91d5-e46e685d1f56', idempotencyKey: 'test-key',
  connection: { provider: 'discord', name: 'discord' }, operation: { connectorId: 'discord', operationId: 'addReaction' },
  context: { cancellationSignal: new AbortController().signal } } as Call;
const channelId = '100000000000000001';
const messageId = '300000000000000001';
const credential = { resolve: async () => ({ botToken: new SecretString('MTAxTESTbot.token-part.0123456789abcdef') }) };

/** A first createMessage attempt whose single-dispatch fence starts without a checkpoint. */
function createMessageCall(): Call {
  let heartbeat: unknown;
  const context = {
    attempt: 1, runId: 'run', flowId: 'channel-announcement-flow', flowStartedAt: new Date('1970-01-01T00:00:01Z'),
    deadline: new Date(Date.now() + 30_000), cancellationSignal: new AbortController().signal,
    hasLastHeartbeatValue: () => heartbeat !== undefined, getLastHeartbeatValue: () => heartbeat,
    recordHeartbeat: async (value: unknown) => { heartbeat = value; },
  };
  return { ...call, operation: { connectorId: 'discord', operationId: 'createMessage' }, context } as unknown as Call;
}

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

it.each([
  { error: new ObjectNotFoundError(), branch: 'defect' },
  { error: new ConnectionNotSelectedError(), branch: 'defect' },
  { error: new CredentialBrokerRejectedError(), branch: 'defect' },
  { error: new CredentialMaterialError('credential bot_token is required'), branch: 'defect' },
  { error: new TypeError('project credential decoding failed'), branch: 'defect' },
  { error: new CredentialBrokerUnavailableError(), branch: undefined },
  { error: new Error('connection is not configured'), branch: undefined },
  { error: new TypeError('fetch failed'), branch: undefined },
])('retries a transient credential failure and keeps a terminal one: $error.name', async ({ error, branch }) => {
  let requestCount = 0;
  const client = new DiscordClient({}, { resolve: async () => { throw error; } }, {
    send: async () => { requestCount++; throw new Error('unexpected request'); },
  });
  const result = await client.createMessage().invoke(createMessageCall(), { channelId, content: 'hello' });
  if (branch) expect(result).toMatchObject({ kind: 'branch', branch });
  else expect(result).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
  expect(requestCount).toBe(0);
});

it.each([[302, 'retry'], [425, 'retry'], [501, 'retry'], [200, 'uncertain'], [422, 'providerRejected']])('classifies a createMessage HTTP %i answer as %s', async (status, expected) => {
  const client = new DiscordClient({}, credential, { send: async () => ({ status, headers: {}, body: Buffer.from('{"code":0}') }) });
  const result = await client.createMessage().invoke(createMessageCall(), { channelId, content: 'hello' });
  expect(result.kind === 'branch' ? result.branch : result.kind).toBe(expected);
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

it('matches Go TrimSpace for message content and reaction emoji', async () => {
  let requestCount = 0;
  const client = new DiscordClient({}, credential, { send: async request => {
    requestCount++;
    if (request.method === 'POST') return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({
      id: messageId, channel_id: channelId, content: '\uFEFF', type: 0,
      author: { id: channelId }, timestamp: '2026-01-01T00:00:00Z',
    })) };
    return { status: 204, headers: {}, body: Buffer.alloc(0) };
  } });

  const blank = await client.createMessage().invoke(createMessageCall(), { channelId, content: '\u0085' });
  expect(blank.kind).toBe('branch');
  if (blank.kind === 'branch') expect(blank.branch).toBe('defect');
  expect(requestCount).toBe(0);

  const message = await client.createMessage().invoke(createMessageCall(), { channelId, content: '\uFEFF' });
  expect(message.kind).toBe('branch');
  if (message.kind === 'branch') expect(message.branch).toBe('created');
  expect(requestCount).toBe(1);

  const spacedEmoji = await client.addReaction().invoke(call, { channelId, messageId, emoji: '\u0085📣' });
  expect(spacedEmoji.kind).toBe('branch');
  if (spacedEmoji.kind === 'branch') expect(spacedEmoji.branch).toBe('defect');
  expect(requestCount).toBe(1);

  const emoji = await client.addReaction().invoke(call, { channelId, messageId, emoji: '\uFEFF📣' });
  expect(emoji.kind).toBe('branch');
  if (emoji.kind === 'branch') expect(emoji.branch).toBe('added');
  expect(requestCount).toBe(2);
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
