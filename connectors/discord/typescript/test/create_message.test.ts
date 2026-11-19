// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { DiscordClient } from '../src/client.js';
import { CreateMessageDefinition } from '../src/generated/definitions.js';
import type { Message } from '../src/generated/models.js';

const callId = '9e237e1d-1296-5851-b23a-ee559a9b4208';
const channelId = '112233445566778899';
const messageId = '998877665544332211';
const sendCap = CreateMessageDefinition.maxSends!;

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readError: Error | undefined;
  writeError: Error | undefined;
  readonly runId = 'run';
  readonly flowId = 'channel-announcement-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  hasLastHeartbeatValue(): boolean {
    if (this.readError) throw this.readError;
    return this.marker !== undefined;
  }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> {
    if (this.writeError) throw this.writeError;
    this.marker = value;
  }
}

interface DiscordAnswer { status: number; body: string; bodyError?: 'transport' | 'stalled' | 'tooLarge' | 'notSent' | 'afterSend' }

const created: DiscordAnswer = { status: 200, body: JSON.stringify({
  id: messageId, channel_id: channelId, content: 'Release @everyone', type: 0, timestamp: '2026-10-13T20:00:00.123000+00:00',
  author: { id: '123456789012345678', username: 'dex-bot', bot: true },
}) };
const serverFailure: DiscordAnswer = { status: 500, body: '{"code":0}' };

function newClient(answers: DiscordAnswer[], resolveError?: Error) {
  const requests: ProviderRequest[] = [];
  const resolve = async () => {
    if (resolveError) throw resolveError;
    return { botToken: new SecretString('MTAxTESTbot.token-part.0123456789abcdef') };
  };
  const client = new DiscordClient({}, { resolve }, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body, bodyError } = answers[Math.min(requests.length - 1, answers.length - 1)]!;
      if (bodyError === 'notSent') throw new ProviderTransportError(true);
      if (bodyError === 'afterSend') throw new ProviderTransportError(false);
      if (bodyError === 'transport') throw new ProviderTransportError(false, status, {});
      if (bodyError === 'stalled') throw new ProviderStalledError(status, {});
      if (bodyError === 'tooLarge') throw new BodyTooLargeError(status, {});
      return { status, headers: {}, body: Buffer.from(body) };
    },
  });
  return { client, requests };
}

async function create(client: DiscordClient, context: AttemptContext, maxSendsOverride?: number): Promise<MutationResult<Message> | RetryError> {
  const call = { id: callId, idempotencyKey: callId, connection: { provider: 'discord', name: 'discord' },
    operation: CreateMessageDefinition.operation, context, ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try { return await runMutation(client.createMessage(), call, { channelId, content: 'Release @everyone' }); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

/** Every resend repeats the request with the same enforced nonce. */
function expectSameEnforcedNonce(requests: ProviderRequest[]): void {
  const bodies = requests.map(request => JSON.parse(Buffer.from(request.body!).toString('utf8')) as Record<string, unknown>);
  expect(bodies.length).toBeGreaterThan(0);
  for (const body of bodies) expect(body).toEqual(bodies[0]);
  expect(bodies[0]!.enforce_nonce).toBe(true);
  expect(bodies[0]!.nonce).toHaveLength(25);
}

describe('Discord bounded resend of unconfirmed messages', () => {
  it.each([
    { name: 'HTTP 500', answer: serverFailure },
    { name: 'HTTP 408', answer: { status: 408, body: '{}' } },
    { name: 'HTTP 409', answer: { status: 409, body: '{}' } },
    { name: 'HTTP 425', answer: { status: 425, body: '{}' } },
    { name: 'connection lost after send', answer: { status: 0, body: '', bodyError: 'afterSend' as const } },
    { name: 'error body naming a message', answer: { status: 403, body: created.body } },
    { name: 'oversized 403 body', answer: { status: 403, body: '', bodyError: 'tooLarge' as const } },
  ])('resends an unknown outcome ($name) with the same nonce below the send cap', async ({ answer }) => {
    const { client, requests } = newClient([answer, created]);
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed', sendCount: 1 });
    context.attempt = 2;
    expect(await create(client, context)).toMatchObject({ branch: 'created', value: { id: messageId } });
    expect(requests).toHaveLength(2);
    expectSameEnforcedNonce(requests);
  });

  it('selects uncertain at the send cap and never sends again', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    for (let attempt = 1; attempt < sendCap; attempt++) {
      context.attempt = attempt;
      expect(await create(client, context)).toBeInstanceOf(RetryError);
    }
    context.attempt = sendCap;
    expect(await create(client, context)).toMatchObject({ branch: 'uncertain' });
    context.attempt++;
    expect(await create(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(sendCap);
    expectSameEnforcedNonce(requests);
  });

  it.each([
    { name: 'unparseable', answer: { status: 200, body: '{' } },
    { name: 'invalid message ID', answer: { status: 200, body: '{"id":"x"}' } },
    { name: 'message ID not text', answer: { status: 200, body: `{"id":${messageId}}` } },
    { name: 'message ID key case', answer: { status: 200, body: `{"ID":"${messageId}"}` } },
    { name: 'broken body', answer: { status: 200, body: '', bodyError: 'transport' as const } },
    { name: 'stalled body', answer: { status: 200, body: '', bodyError: 'stalled' as const } },
    { name: 'oversized body', answer: { status: 200, body: '', bodyError: 'tooLarge' as const } },
  ])('selects uncertain without resending an accepted answer without a message ID ($name)', async ({ answer }) => {
    const { client, requests } = newClient([answer, created]);
    expect(await create(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('treats an error body over 64 KiB as an unknown outcome even below maxResponseBytes', async () => {
    const { client, requests } = newClient([{ status: 400, body: JSON.stringify({ code: 50035, message: 'x'.repeat(65_536) }) }]);
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed' });
    expect(requests).toHaveLength(1);
  });

  it('honors an application send cap below the manifest cap', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    expect(await create(client, context, 2)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await create(client, context, 2)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(2);
  });

  it.each([
    { status: 404, body: '{"code":10003}', branch: 'notFound' },
    { status: 403, body: '{"code":50013}', branch: 'providerRejected' },
    { status: 400, body: '{"code":50035}', branch: 'providerRejected' },
    { status: 401, body: '{"code":0}', branch: 'providerRejected' },
    { status: 422, body: '{"code":50035}', branch: 'providerRejected' },
  ])('never resends a definite HTTP $status refusal of the first send', async ({ status, body, branch }) => {
    const { client, requests } = newClient([{ status, body }]);
    const context = new AttemptContext();
    expect(await create(client, context)).toMatchObject({ branch });
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain for a refusal after a possible earlier send', async () => {
    const { client } = newClient([serverFailure, { status: 403, body: '{"code":50013}' }]);
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await create(client, context)).toMatchObject({ branch: 'uncertain', failure: { kind: 'AUTHORIZATION' } });
  });

  it('retries rate limits, outages, and unsent requests without counting a send', async () => {
    const { client, requests } = newClient([
      { status: 429, body: '{"retry_after":1.5}' }, { status: 503, body: '{"code":0}' },
      { status: 0, body: '', bodyError: 'notSent' }, created,
    ]);
    const context = new AttemptContext();
    for (let attempt = 1; attempt <= 3; attempt++) {
      context.attempt = attempt;
      expect(await create(client, context)).toBeInstanceOf(RetryError);
      expect(context.marker).toMatchObject({ state: 'canResend' });
    }
    context.attempt = 4;
    expect(await create(client, context)).toMatchObject({ branch: 'created' });
    expectSameEnforcedNonce(requests);
  });

  it('retries without sending when the checkpoint is unreadable', async () => {
    const { client, requests } = newClient([created]);
    const context = new AttemptContext();
    context.readError = new Error('heartbeat unavailable');
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(0);
  });

  it('selects uncertain for a terminal credential failure after a possible send', async () => {
    const first = newClient([serverFailure]);
    const context = new AttemptContext();
    expect(await create(first.client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const revoked = newClient([serverFailure], new ReauthorizationRequiredError());
    expect(await create(revoked.client, context)).toMatchObject({ branch: 'uncertain' });
    expect(revoked.requests).toHaveLength(0);
  });

  it('records notSent and retries a transient credential failure before any send', async () => {
    const { client, requests } = newClient([created], new Error('broker down'));
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'notSent' });
    expect(requests).toHaveLength(0);
  });
});

const incompleteAccepted: DiscordAnswer = { status: 200, body: JSON.stringify({ id: messageId, channel_id: channelId }) };
const readBackPath = `/channels/${channelId}/messages/${messageId}`;

/** Answer the POST with an incomplete accepted message and each GET with the next read-back answer. */
function newReadBackClient(readBacks: DiscordAnswer[], options: { onPost?: () => void; resolveError?: Error } = {}) {
  const requests: ProviderRequest[] = [];
  let readBackCount = 0;
  const resolve = async () => {
    if (options.resolveError) throw options.resolveError;
    return { botToken: new SecretString('MTAxTESTbot.token-part.0123456789abcdef') };
  };
  const client = new DiscordClient({}, { resolve }, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      if (request.method === 'POST') {
        options.onPost?.();
        return { status: incompleteAccepted.status, headers: {}, body: Buffer.from(incompleteAccepted.body) };
      }
      const { status, body } = readBacks[Math.min(readBackCount++, readBacks.length - 1)]!;
      return { status, headers: {}, body: Buffer.from(body) };
    },
  });
  return { client, requests };
}

function expectOnePostThenReadBacks(requests: ProviderRequest[], readBackCount: number): void {
  expect(requests).toHaveLength(1 + readBackCount);
  expect(requests[0]!.method).toBe('POST');
  for (const request of requests.slice(1)) {
    expect(request.method, 'an accepted message is never resent').toBe('GET');
    expect(new URL(request.url).pathname).toBe(`/api/v10${readBackPath}`);
  }
}

describe('Discord read-back of an accepted message', () => {
  it('reads the message back by the ID of an accepted answer', async () => {
    const { client, requests } = newReadBackClient([created]);
    const context = new AttemptContext();
    expect(await create(client, context)).toMatchObject({ branch: 'created', value: { id: messageId } });
    expect(JSON.stringify(context.marker)).toContain(`"providerId":"${messageId}"`);
    expectOnePostThenReadBacks(requests, 1);
  });

  it.each([{ maxSendsOverride: undefined }, { maxSendsOverride: 1 }])('retries an unknown read-back with the saved ID and never resends (cap $maxSendsOverride)', async ({ maxSendsOverride }) => {
    const { client, requests } = newReadBackClient([{ status: 503, body: '{"code":0}' }, serverFailure, created]);
    const context = new AttemptContext();
    for (const attempt of [1, 2]) {
      context.attempt = attempt;
      expect(await create(client, context, maxSendsOverride)).toBeInstanceOf(RetryError);
      expect(JSON.stringify(context.marker)).toContain(`"providerId":"${messageId}"`);
    }
    context.attempt = 3;
    expect(await create(client, context, maxSendsOverride)).toMatchObject({ branch: 'created', value: { id: messageId } });
    expectOnePostThenReadBacks(requests, 3);
  });

  it.each([
    { name: 'unknown message', answer: { status: 404, body: '{"code":10008}' } },
    { name: 'missing message history', answer: { status: 403, body: '{"code":50001}' } },
  ])('selects uncertain for a definite read-back refusal ($name)', async ({ answer }) => {
    const { client, requests } = newReadBackClient([answer]);
    expect(await create(client, new AttemptContext())).toMatchObject({ branch: 'uncertain', receipt: { providerObjectId: messageId } });
    expectOnePostThenReadBacks(requests, 1);
  });

  it('retries a contradicting read-back with the saved ID', async () => {
    const contradicting = { status: 200, body: created.body.replace(`"id":"${messageId}"`, '"id":"998877665544332212"') };
    const { client, requests } = newReadBackClient([contradicting]);
    expect(await create(client, new AttemptContext())).toBeInstanceOf(RetryError);
    expectOnePostThenReadBacks(requests, 1);
  });

  it('selects uncertain for an unknown read-back when the message ID was not saved', async () => {
    const context = new AttemptContext();
    // The claim is already persisted, so only the message ID checkpoint fails.
    const { client, requests } = newReadBackClient([{ status: 503, body: '{"code":0}' }], { onPost: () => { context.writeError = new Error('heartbeat unavailable'); } });
    expect(await create(client, context)).toMatchObject({ branch: 'uncertain' });
    expectOnePostThenReadBacks(requests, 1);
  });

  it('never resends a saved message ID after a credential failure', async () => {
    const first = newReadBackClient([{ status: 503, body: '{"code":0}' }]);
    const context = new AttemptContext();
    expect(await create(first.client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const unavailable = newReadBackClient([created], { resolveError: new Error('broker timeout') });
    expect(await create(unavailable.client, context)).toBeInstanceOf(RetryError);
    context.attempt = 3;
    const revoked = newReadBackClient([created], { resolveError: new ReauthorizationRequiredError() });
    expect(await create(revoked.client, context)).toMatchObject({ branch: 'uncertain' });
    expect(unavailable.requests).toHaveLength(0);
    expect(revoked.requests).toHaveLength(0);
    expectOnePostThenReadBacks(first.requests, 1);
  });
});
