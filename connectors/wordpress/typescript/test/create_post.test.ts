// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { WordPressClient } from '../src/client.js';
import { CreatePostDefinition } from '../src/generated/definitions.js';
import type { CreatedPost } from '../src/generated/models.js';

const callId = '053fca46-6608-58b2-a693-d0aefe234186';
const sendCap = CreatePostDefinition.maxSends!;

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readError: Error | undefined;
  readonly runId = 'run';
  readonly flowId = 'wordpress-create-flow';
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

interface WordPressAnswer { status: number; body: string }

const created: WordPressAnswer = { status: 201, body: '{"id":43,"status":"draft"}' };
const serverFailure: WordPressAnswer = { status: 500, body: '{"code":"internal_server_error"}' };

function newClient(answers: WordPressAnswer[], username = 'editor') {
  const requests: ProviderRequest[] = [];
  const resolve = async () => ({ username, applicationPassword: new SecretString('abcd efgh') });
  const client = new WordPressClient({ siteUrl: 'http://127.0.0.1:12345' } as never, { resolve }, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body } = answers[Math.min(requests.length - 1, answers.length - 1)]!;
      return { status, headers: {}, body: Buffer.from(body) };
    },
  });
  return { client, requests };
}

async function create(client: WordPressClient, context: AttemptContext, maxSendsOverride?: number): Promise<MutationResult<CreatedPost> | RetryError> {
  const call = { id: callId, connection: { provider: 'wordpress', name: 'wordpress' }, operation: CreatePostDefinition.operation, context, ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try { return await runMutation(client.createPost(), call, { title: 'Meridian release', content: '<p>Draft.</p>', status: '' }); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

describe('WordPress bounded resend of unconfirmed posts', () => {
  it.each([500, 502, 408, 409, 425, 418])('resends an unknown HTTP %i outcome below the send cap', async status => {
    const { client, requests } = newClient([{ status, body: '{"code":"error"}' }, created]);
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed', sendCount: 1 });
    context.attempt = 2;
    expect(await create(client, context)).toMatchObject({ branch: 'created' });
    expect(context.marker).toMatchObject({ sendCount: 2 });
    expect(requests).toHaveLength(2);
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
    expect(requests).toHaveLength(3);
  });

  it('never resends when a Step sets the send cap to 1', async () => {
    const { client, requests } = newClient([serverFailure]);
    expect(await create(client, new AttemptContext(), 1)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('resends a malformed success response below the send cap', async () => {
    const { client } = newClient([{ status: 201, body: 'not JSON' }, created]);
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await create(client, context)).toMatchObject({ branch: 'created' });
  });

  it.each([400, 401, 403, 404, 413, 422])('never resends a definite HTTP %i refusal', async status => {
    const { client, requests } = newClient([{ status, body: '{"code":"rest_error"}' }]);
    const context = new AttemptContext();
    expect(await create(client, context)).toMatchObject({ branch: 'providerRejected' });
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain for a refusal of a resend after a possible send', async () => {
    const { client, requests } = newClient([serverFailure, { status: 400, body: '{"code":"rest_invalid_param"}' }]);
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await create(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(2);
  });

  it('never resends an accepted but unexpected response', async () => {
    const { client, requests } = newClient([{ status: 200, body: '{"id":43,"status":"trash"}' }]);
    expect(await create(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('never resends a confirmed post', async () => {
    const { client, requests } = newClient([created]);
    const context = new AttemptContext();
    expect(await create(client, context)).toMatchObject({ branch: 'created' });
    context.attempt = 2;
    expect(await create(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('returns Retry when the dispatch checkpoint is unreadable', async () => {
    const { client, requests } = newClient([created]);
    const context = new AttemptContext();
    context.readError = new Error('heartbeat unavailable');
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(0);
  });

  it('retries a credential failure after a possible send and selects defect before any send', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const { client: invalidClient } = newClient([serverFailure], 'a:b');
    expect(await create(invalidClient, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(1);
    expect(await create(invalidClient, new AttemptContext())).toMatchObject({ branch: 'defect' });
  });
});
