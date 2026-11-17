// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { FacebookPagesClient } from '../src/client.js';
import { PublishTextPostDefinition } from '../src/generated/definitions.js';
import type { PublishedPost } from '../src/generated/models.js';

const callId = 'd2357ac8-f73e-5df4-952e-b68da926f540';
const sendCap = PublishTextPostDefinition.maxSends!;

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readError: Error | undefined;
  readonly runId = 'run';
  readonly flowId = 'facebook-pages-publish-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  hasLastHeartbeatValue(): boolean {
    if (this.readError) throw this.readError;
    return this.marker !== undefined;
  }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

interface MetaAnswer { status: number; body: string }

const published: MetaAnswer = { status: 200, body: '{"id":"123456789_987654321"}' };
const serverFailure: MetaAnswer = { status: 500, body: '{"error":{"message":"unknown"}}' };

function newClient(answers: MetaAnswer[], token = 'synthetic-page-token') {
  const requests: ProviderRequest[] = [];
  const resolve = async () => ({ pageAccessToken: new SecretString(token) });
  const client = new FacebookPagesClient({ baseUrl: 'http://127.0.0.1:12345/v25.0' } as never, { resolve }, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body } = answers[Math.min(requests.length - 1, answers.length - 1)]!;
      return { status, headers: {}, body: Buffer.from(body) };
    },
  });
  return { client, requests };
}

async function publish(client: FacebookPagesClient, context: AttemptContext, maxSendsOverride?: number): Promise<MutationResult<PublishedPost> | RetryError> {
  const call = { id: callId, connection: { provider: 'facebook-pages', name: 'facebook-pages' }, operation: PublishTextPostDefinition.operation, context, ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try { return await runMutation(client.publishTextPost(), call, { pageId: '123456789', message: 'Hello from Dex' }); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

describe('Facebook Pages bounded resend of unconfirmed posts', () => {
  it.each([[500, ''], [502, ''], [408, ''], [409, ''], [425, ''], [418, ''], [200, '{}']])('resends an unknown HTTP %i outcome below the send cap', async (status, body) => {
    const { client, requests } = newClient([{ status, body }, published]);
    const context = new AttemptContext();
    expect(await publish(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed', sendCount: 1 });
    context.attempt = 2;
    expect(await publish(client, context)).toMatchObject({ branch: 'published' });
    expect(context.marker).toMatchObject({ sendCount: 2 });
    expect(requests).toHaveLength(2);
  });

  it('selects uncertain at the send cap and never sends again', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    for (let attempt = 1; attempt < sendCap; attempt++) {
      context.attempt = attempt;
      expect(await publish(client, context)).toBeInstanceOf(RetryError);
    }
    context.attempt = sendCap;
    expect(await publish(client, context)).toMatchObject({ branch: 'uncertain' });
    context.attempt++;
    expect(await publish(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(3);
  });

  it('never resends when a Step sets the send cap to 1', async () => {
    const { client, requests } = newClient([serverFailure]);
    expect(await publish(client, new AttemptContext(), 1)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it.each([400, 401, 403, 404, 413, 422])('never resends a definite HTTP %i refusal', async status => {
    const { client, requests } = newClient([{ status, body: '{"error":{"code":100}}' }]);
    const context = new AttemptContext();
    expect(await publish(client, context)).toMatchObject({ branch: 'providerRejected' });
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it('retries a Graph throttle after checkpointing canResend', async () => {
    const { client } = newClient([{ status: 400, body: '{"error":{"code":613}}' }]);
    const context = new AttemptContext();
    expect(await publish(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'canResend' });
  });

  it('selects uncertain for a refusal of a resend after a possible send', async () => {
    const { client, requests } = newClient([serverFailure, { status: 400, body: '{"error":{"code":100}}' }]);
    const context = new AttemptContext();
    expect(await publish(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await publish(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(2);
  });

  it('completes from a checkpointed post ID without resending', async () => {
    const { client, requests } = newClient([published]);
    const context = new AttemptContext();
    expect(await publish(client, context)).toMatchObject({ branch: 'published' });
    context.attempt = 2;
    expect(await publish(client, context)).toMatchObject({ branch: 'published', value: { postId: '123456789_987654321' } });
    expect(requests).toHaveLength(1);
  });

  it('returns Retry when the dispatch checkpoint is unreadable', async () => {
    const { client, requests } = newClient([published]);
    const context = new AttemptContext();
    context.readError = new Error('heartbeat unavailable');
    expect(await publish(client, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(0);
  });

  it('retries a credential failure after a possible send and selects defect before any send', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    expect(await publish(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const { client: invalidClient } = newClient([serverFailure], 'bad\ntoken');
    expect(await publish(invalidClient, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(1);
    expect(await publish(invalidClient, new AttemptContext())).toMatchObject({ branch: 'defect' });
  });
});
