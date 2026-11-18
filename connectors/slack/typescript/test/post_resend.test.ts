// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { SlackClient } from '../src/client.js';
import { PostChannelMessageDefinition } from '../src/generated/definitions.js';
import type { PostMessageOutput } from '../src/generated/models.js';

const callId = '74390522-858d-5ed6-bdf7-04e979ba9e4f';

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'post-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

interface SlackAnswer { status: number; body: string }

function newClient(answer: (request: ProviderRequest) => SlackAnswer | Error, botTokens: string[] = ['bot-token']) {
  const requests: ProviderRequest[] = [];
  let resolutions = 0;
  const resolve = async () => {
    const botToken = botTokens[Math.min(resolutions++, botTokens.length - 1)] ?? '';
    return { botToken: new SecretString(botToken), userToken: new SecretString('user-token') };
  };
  const client = new SlackClient({}, { resolve, resolveWithRefresh: resolve }, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const answered = answer(request);
      if (answered instanceof Error) throw answered;
      return { status: answered.status, headers: {}, body: Buffer.from(answered.body) };
    },
  });
  return { client, requests };
}

/** Read this call's marker from a flat or per-call map heartbeat. */
function callMarker(heartbeat: unknown): Record<string, unknown> | undefined {
  const value = heartbeat as { singleDispatchMarkers?: Record<string, Record<string, unknown>> } & Record<string, unknown> | undefined;
  return value?.singleDispatchMarkers ? value.singleDispatchMarkers[callId] : value;
}

async function post(client: SlackClient, context: AttemptContext, maxSendsOverride?: number): Promise<MutationResult<PostMessageOutput> | RetryError> {
  const call = { id: callId, connection: { provider: 'slack', name: 'workspace' }, operation: PostChannelMessageDefinition.operation, context, ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try { return await runMutation(client.postChannelMessage(), call, { channelId: 'C123', text: 'hello' }); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

const serverFailure: SlackAnswer = { status: 500, body: '{"ok":false,"error":"fatal_error"}' };

describe('Slack bounded resend of unconfirmed posts', () => {
  it('resends an unknown outcome until the generated send cap, then selects uncertain', async () => {
    const { client, requests } = newClient(() => serverFailure);
    const context = new AttemptContext();
    for (let attempt = 1; attempt < PostChannelMessageDefinition.maxSends!; attempt++) {
      context.attempt = attempt;
      expect(await post(client, context)).toBeInstanceOf(RetryError);
      expect(context.marker).toMatchObject({ state: 'claimed', sendCount: attempt });
    }
    context.attempt = PostChannelMessageDefinition.maxSends!;
    expect(await post(client, context)).toMatchObject({ branch: 'uncertain' });
    context.attempt++;
    expect(await post(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(3);
    const messageIds = requests.map(request => JSON.parse(Buffer.from(request.body!).toString()).client_msg_id);
    expect(new Set(messageIds)).toEqual(new Set([callId]));
  });

  it('never resends when a Step sets the send cap to 1', async () => {
    const { client, requests } = newClient(() => serverFailure);
    expect(await post(client, new AttemptContext(), 1)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it.each([
    { status: 200, body: '{"ok":false,"error":"channel_not_found"}' },
    { status: 200, body: '{"ok":false,"error":"not_in_channel"}' },
    { status: 400, body: '{"ok":false,"error":"invalid_arguments"}' },
    { status: 403, body: '{"ok":false,"error":"restricted_action"}' },
    { status: 401, body: '{"ok":false,"error":"invalid_auth"}' },
  ])('never resends a definite refusal and records canResend for $status $body', async refusal => {
    const { client, requests } = newClient(() => refusal);
    const context = new AttemptContext();
    expect(await post(client, context)).toMatchObject({ branch: 'providerRejected' });
    expect(requests).toHaveLength(1);
    // canResend proves Slack posted nothing, so agent hosts classify the refusal as failed.
    expect(context.marker).toMatchObject({ callId, attempt: 1, state: 'canResend' });
    const marker = context.marker as { sendCount?: number; providerId?: string };
    expect(marker.sendCount ?? 0).toBe(0);
    expect(marker.providerId).toBeUndefined();
  });

  it.each([
    { status: 200, body: '{"ok":false}' },
    { status: 200, body: '{}' },
    { status: 408, body: '{"ok":false,"error":"request_timeout"}' },
    { status: 418, body: '{"ok":false,"error":"teapot"}' },
    { status: 400, body: '{"ok":true,"channel":"C123"}' },
    { status: 403, body: '{"ok":true,"channel":"C123"}' },
    { status: 401, body: '{"ok":true,"channel":"C123"}' },
  ])('resends a non-definite refusal $status $body until the send cap', async unknown => {
    const { client, requests } = newClient(() => unknown);
    const context = new AttemptContext();
    const states: unknown[] = [];
    for (let attempt = 1; attempt < PostChannelMessageDefinition.maxSends!; attempt++) {
      context.attempt = attempt;
      expect(await post(client, context)).toBeInstanceOf(RetryError);
      states.push((context.marker as { state?: string }).state);
    }
    context.attempt = PostChannelMessageDefinition.maxSends!;
    // The outcome is unknown, so the post repeats with one client_msg_id until the cap.
    expect(await post(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(PostChannelMessageDefinition.maxSends!);
    const messageIds = requests.map(request => JSON.parse(Buffer.from(request.body!).toString()).client_msg_id);
    expect(new Set(messageIds)).toEqual(new Set([callId]));
    expect(context.marker).toMatchObject({ state: 'claimed', sendCount: PostChannelMessageDefinition.maxSends! });
    expect(states).not.toContain('canResend');
  });

  it('reads back a client error that names a message timestamp without recording canResend', async () => {
    const { client, requests } = newClient(request => request.method === 'GET'
      ? { status: 200, body: '{"ok":true,"messages":[{"ts":"2.0","user":"UBOT","text":"hello"}]}' }
      : { status: 400, body: '{"ok":true,"channel":"C123","ts":"2.0"}' });
    const context = new AttemptContext();
    expect(await post(client, context)).toMatchObject({ branch: 'sent' });
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET']);
    expect(context.marker).toMatchObject({ state: 'claimed', providerId: '2.0' });
  });

  it('selects providerRejected when the canResend checkpoint is lost', async () => {
    const { client, requests } = newClient(() => ({ status: 200, body: '{"ok":false,"error":"channel_not_found"}' }));
    const context = new AttemptContext();
    context.recordHeartbeat = async (value: unknown) => {
      if ((value as { state?: string }).state === 'canResend') throw new Error('can-resend heartbeat unavailable');
      context.marker = value;
    };
    expect(await post(client, context)).toMatchObject({ branch: 'providerRejected' });
    expect(requests).toHaveLength(1);
    expect(context.marker).toMatchObject({ state: 'claimed' });
  });

  it('selects uncertain for a definite refusal of a resend after a possible send', async () => {
    let posts = 0;
    const { client, requests } = newClient(() => (++posts === 1 ? serverFailure : { status: 200, body: '{"ok":false,"error":"channel_not_found"}' }));
    const context = new AttemptContext();
    expect(await post(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await post(client, context)).toMatchObject({ branch: 'uncertain', failure: { kind: 'NOT_FOUND' } });
    expect(requests).toHaveLength(2);
    expect(context.marker).toMatchObject({ state: 'claimed', sendCount: 2 });
  });

  it('selects uncertain when terminal credentials block a send after a counted canResend', async () => {
    const { client, requests } = newClient(() => serverFailure, ['']);
    const context = new AttemptContext();
    context.attempt = 3;
    // A canResend marker that still counts one possible send allows a send without read-back.
    context.marker = { callId, attempt: 2, state: 'canResend', sendCount: 1 };
    expect(await post(client, context)).toMatchObject({ branch: 'uncertain', failure: { kind: 'AUTHENTICATION' } });
    expect(requests).toHaveLength(0);
  });

  it.each([1, 2, 3, 9])('retries an unreadable checkpoint at attempt %d', async attempt => {
    const { client, requests } = newClient(() => serverFailure);
    const context = new AttemptContext();
    context.attempt = attempt;
    context.marker = { callId: '5f0c4d1e-8b9a-4c3d-9e2f-1a2b3c4d5e6f', attempt: 1, state: 'canResend' };
    // The retry budget, not the send cap, bounds a fail-closed checkpoint.
    expect(await post(client, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(0);
  });

  it.each(['{"error":"channel_not_found"}', '{"error":"invalid_auth"}', '{"error":"ratelimited"}'])('treats a 2xx %s without ok as an unknown outcome', async body => {
    const { client, requests } = newClient(() => ({ status: 200, body }));
    const context = new AttemptContext();
    const states: unknown[] = [];
    for (let attempt = 1; attempt < PostChannelMessageDefinition.maxSends!; attempt++) {
      context.attempt = attempt;
      expect(await post(client, context)).toBeInstanceOf(RetryError);
      states.push(callMarker(context.marker)?.state);
    }
    context.attempt = PostChannelMessageDefinition.maxSends!;
    // Without an explicit ok:false the post may exist, so every send counts toward the cap.
    expect(await post(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(PostChannelMessageDefinition.maxSends!);
    expect(states).not.toContain('canResend');
  });

  it.each([
    '{"ok":false,"error":"channel_not_found","ts":"2.0"}',
    '{"ok":false,"error":"invalid_auth","ts":"2.0"}',
    '{"ok":false,"error":"ratelimited","message":{"ts":"2.0"}}',
    '{"ts":"2.0"}',
  ])('reads back a 2xx refusal %s that names a timestamp', async body => {
    const { client, requests } = newClient(request => request.method === 'GET'
      ? { status: 200, body: '{"ok":true,"messages":[{"ts":"2.0","user":"UBOT","text":"hello"}]}' }
      : { status: 200, body });
    const context = new AttemptContext();
    // A timestamp contradicts the refusal, so the post is read back and never resent.
    expect(await post(client, context)).toMatchObject({ branch: 'sent' });
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET']);
    expect(callMarker(context.marker)).toMatchObject({ state: 'claimed', providerId: '2.0' });
  });

  it.each([
    { name: 'transport', error: new ProviderTransportError(false, 200, {}) },
    { name: 'stalled', error: new ProviderStalledError(200, {}) },
  ])('never resends a 2xx whose body fails with a $name error', async ({ error }) => {
    const { client, requests } = newClient(() => error);
    expect(await post(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it.each([
    { name: 'a non-JSON body', answer: (): SlackAnswer | Error => ({ status: 409, body: 'conflict' }) },
    { name: 'an oversized body', answer: (): SlackAnswer | Error => new BodyTooLargeError(409, {}) },
    { name: 'a body broken mid-read', answer: (): SlackAnswer | Error => new ProviderTransportError(false, 409, {}) },
    { name: 'a stalled body', answer: (): SlackAnswer | Error => new ProviderStalledError(409, {}) },
  ])('never resends a 409 with $name', async ({ answer }) => {
    const { client, requests } = newClient(answer);
    expect(await post(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('reads back a 409 that names a message timestamp', async () => {
    const { client, requests } = newClient(request => request.method === 'GET'
      ? { status: 200, body: '{"ok":true,"messages":[{"ts":"2.0","user":"UBOT","text":"hello"}]}' }
      : { status: 409, body: '{"ok":false,"error":"conflict","ts":"2.0"}' });
    const context = new AttemptContext();
    expect(await post(client, context)).toMatchObject({ branch: 'sent' });
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET']);
    expect(callMarker(context.marker)).toMatchObject({ state: 'claimed', providerId: '2.0' });
  });

  it('keeps counting an unreadable 401 send after its credential refresh succeeds', async () => {
    const requests: ProviderRequest[] = [];
    let forcedRefreshes = 0;
    const credentials = (botToken: string) => ({ botToken: new SecretString(botToken), userToken: new SecretString('user-token') });
    const provider = {
      resolve: async () => credentials('rejected-bot'), resolveWithRefresh: async () => credentials('rejected-bot'),
      resolveAfterRejection: async () => { forcedRefreshes++; return credentials('replacement-bot'); },
    };
    const client = new SlackClient({}, provider, {
      send: async (request: ProviderRequest) => {
        requests.push(request);
        return { status: 401, headers: {}, body: Buffer.from('{"ok":') };
      },
    });
    const context = new AttemptContext();
    // An unreadable rejection cannot prove nothing was posted, so canResend is never recorded.
    expect(await post(client, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(1);
    expect(forcedRefreshes).toBe(1);
    expect(callMarker(context.marker)).toMatchObject({ state: 'claimed', sendCount: 1 });
  });

  it('treats an unreadable 401 body as an unknown outcome', async () => {
    const { client } = newClient(() => ({ status: 401, body: '{"ok":' }));
    const context = new AttemptContext();
    // An unreadable body cannot prove the refusal, so the post repeats below the cap.
    expect(await post(client, context)).toBeInstanceOf(RetryError);
    expect(callMarker(context.marker)).toMatchObject({ state: 'claimed' });
  });

  it('selects uncertain instead of retrying read-back when the timestamp checkpoint is lost', async () => {
    const { client, requests } = newClient(request => request.method === 'GET'
      ? { status: 500, body: '{"ok":false,"error":"internal_error"}' }
      : { status: 200, body: '{"ok":true,"ts":"2.0"}' });
    const context = new AttemptContext();
    context.recordHeartbeat = async (value: unknown) => {
      if (callMarker(value)?.providerId) throw new Error('provider ID heartbeat unavailable');
      context.marker = value;
    };
    // A Retry would leave a bare claim, which permits a resend of the accepted post.
    expect(await post(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET']);
  });

  it('keeps counting an unknown send after a later 429 records canResend', async () => {
    const { client, requests } = newClient(() => (requests.length === 2 ? { status: 429, body: '{"ok":false,"error":"ratelimited"}' } : serverFailure));
    const context = new AttemptContext();
    for (let attempt = 1; attempt <= 3; attempt++) {
      context.attempt = attempt;
      expect(await post(client, context)).toBeInstanceOf(RetryError);
    }
    context.attempt = 4;
    // Slack cannot read back without a timestamp; the 429 is uncounted, the two unknown sends are not.
    expect(await post(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(4);
    expect(callMarker(context.marker)).toMatchObject({ sendCount: 3 });
  });

  it('records nothing when transient credentials block a send after a counted canResend', async () => {
    const resolve = async (): Promise<never> => { throw new Error('credential store unavailable'); };
    const requests: ProviderRequest[] = [];
    const client = new SlackClient({}, { resolve, resolveWithRefresh: resolve }, {
      send: async (request: ProviderRequest) => { requests.push(request); return { status: 500, headers: {}, body: Buffer.from('{}') }; },
    });
    const context = new AttemptContext();
    context.attempt = 3;
    const seeded = { callId, attempt: 2, state: 'canResend', sendCount: 1 };
    context.marker = seeded;
    // notSent would hide the counted first post from the next attempt.
    expect(await post(client, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(0);
    expect(context.marker).toBe(seeded);
  });

  it('selects uncertain when terminal credentials block a resend after a possible send', async () => {
    const { client, requests } = newClient(() => serverFailure, ['bot-token', '']);
    const context = new AttemptContext();
    expect(await post(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const result = await post(client, context);
    expect(result).toMatchObject({ branch: 'uncertain', failure: { kind: 'AUTHENTICATION' } });
    expect(requests).toHaveLength(1);
    expect(context.marker).toMatchObject({ attempt: 1, state: 'claimed' });
  });

  it('reads back a checkpointed timestamp instead of resending it', async () => {
    const { client, requests } = newClient(request => request.method === 'GET'
      ? { status: 200, body: '{"ok":true,"messages":[]}' }
      : { status: 200, body: '{"ok":true,"channel":"C123","ts":"2.0"}' });
    const context = new AttemptContext();
    context.attempt = 2;
    context.marker = { callId, attempt: 1, state: 'claimed', providerId: '2.0', sendCount: 1 };
    expect(await post(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests.map(request => request.method)).toEqual(['GET']);
  });
});
