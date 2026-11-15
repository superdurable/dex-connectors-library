// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
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

function newClient(answer: (request: ProviderRequest) => SlackAnswer, botTokens: string[] = ['bot-token']) {
  const requests: ProviderRequest[] = [];
  let resolutions = 0;
  const resolve = async () => {
    const botToken = botTokens[Math.min(resolutions++, botTokens.length - 1)] ?? '';
    return { botToken: new SecretString(botToken), userToken: new SecretString('user-token') };
  };
  const client = new SlackClient({}, { resolve, resolveWithRefresh: resolve }, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body } = answer(request);
      return { status, headers: {}, body: Buffer.from(body) };
    },
  });
  return { client, requests };
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

  it('never resends a definite refusal', async () => {
    const { client, requests } = newClient(() => ({ status: 200, body: '{"ok":false,"error":"channel_not_found"}' }));
    expect(await post(client, new AttemptContext())).toMatchObject({ branch: 'providerRejected' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain for a definite refusal of a resend after a possible send', async () => {
    let posts = 0;
    const { client, requests } = newClient(() => (++posts === 1 ? serverFailure : { status: 200, body: '{"ok":false,"error":"channel_not_found"}' }));
    const context = new AttemptContext();
    expect(await post(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await post(client, context)).toMatchObject({ branch: 'uncertain', failure: { kind: 'NOT_FOUND' } });
    expect(requests).toHaveLength(2);
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
