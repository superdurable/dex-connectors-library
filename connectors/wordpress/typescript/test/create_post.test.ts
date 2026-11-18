// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it, vi } from 'vitest';
import { DispatchNotAllowedError, Fence } from '@superdurable/connectors-sdk/singledispatch';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import {
  ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError,
} from '@superdurable/connectors-sdk/projectconfig';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { WordPressClient } from '../src/client.js';
import { CreatePostDefinition } from '../src/generated/definitions.js';
import { CredentialMaterialError, type CreatedPost } from '../src/generated/models.js';

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

interface WordPressAnswer { status: number; body: string; bodyError?: 'transport' | 'stalled' | 'tooLarge' }

const created: WordPressAnswer = { status: 201, body: '{"id":43,"status":"draft"}' };
const serverFailure: WordPressAnswer = { status: 500, body: '{"code":"internal_server_error"}' };

function newClient(answers: WordPressAnswer[], username = 'editor', resolveError?: Error) {
  const requests: ProviderRequest[] = [];
  const resolve = async () => {
    if (resolveError) throw resolveError;
    return { username, applicationPassword: new SecretString('abcd efgh') };
  };
  const client = new WordPressClient({ siteUrl: 'http://127.0.0.1:12345' } as never, { resolve }, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body, bodyError } = answers[Math.min(requests.length - 1, answers.length - 1)]!;
      if (bodyError === 'transport') throw new ProviderTransportError(false, status, {});
      if (bodyError === 'stalled') throw new ProviderStalledError(status, {});
      if (bodyError === 'tooLarge') throw new BodyTooLargeError(status, {});
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

  it.each([
    { status: 201, body: 'not JSON' },
    { status: 201, body: '{}' },
    { status: 201, body: '', bodyError: 'transport' },
    { status: 201, body: '', bodyError: 'stalled' },
    { status: 201, body: '', bodyError: 'tooLarge' },
  ] as WordPressAnswer[])('never resends an accepted post whose response is unreadable: %j', async answer => {
    const { client, requests } = newClient([answer, created]);
    expect(await create(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('never resends an unlisted error response that reports a post ID', async () => {
    const { client, requests } = newClient([{ status: 409, body: created.body }]);
    expect(await create(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('treats an unreadable refusal body as an unknown outcome', async () => {
    const { client, requests } = newClient([{ status: 400, body: '', bodyError: 'transport' }, created]);
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed' });
    context.attempt = 2;
    expect(await create(client, context)).toMatchObject({ branch: 'created' });
    expect(requests).toHaveLength(2);
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

  it('returns Retry when the Fence refuses before sending', async () => {
    const { client, requests } = newClient([created]);
    class ShortWindowContext extends AttemptContext {
      override readonly deadline = new Date(Date.now() + 50);
      override async recordHeartbeat(): Promise<void> { throw new Error('heartbeat write failed'); }
    }
    // A cap of 1 makes an unknown outcome select uncertain, so Retry proves nothing was counted as sent.
    expect(await create(client, new ShortWindowContext(), 1)).toBeInstanceOf(RetryError);
    const refusal = vi.spyOn(Fence.prototype, 'dispatch').mockRejectedValue(new DispatchNotAllowedError());
    try { expect(await create(client, new AttemptContext(), 1)).toBeInstanceOf(RetryError); }
    finally { refusal.mockRestore(); }
    expect(requests).toHaveLength(0);
  });

  it('getPost and listPosts retry a transient credential failure and select defect only for a terminal one', async () => {
    const context = new AttemptContext();
    const call = (operationId: string) => ({ id: callId, connection: { provider: 'wordpress', name: 'wordpress' }, operation: { connectorId: 'wordpress', operationId }, context }) as unknown as Call;
    const transient = [new CredentialBrokerUnavailableError(), new Error('connection is not configured')];
    const terminal = [new ReauthorizationRequiredError(), new ObjectNotFoundError(), new ConnectionNotSelectedError(), new CredentialBrokerRejectedError(), new CredentialMaterialError('username is required')];
    for (const error of [...transient, ...terminal]) {
      const { client, requests } = newClient([created], undefined, error);
      const results = [await client.getPost().invoke(call('getPost'), { postId: 42 }), await client.listPosts().invoke(call('listPosts'), {})];
      for (const result of results) {
        if (transient.includes(error)) expect(result.kind).toBe('retry');
        else expect(result).toMatchObject({ kind: 'branch', branch: 'defect' });
      }
      expect(requests).toHaveLength(0);
    }
  });

  it('returns Retry when the dispatch checkpoint is unreadable', async () => {
    const { client, requests } = newClient([created]);
    const context = new AttemptContext();
    context.readError = new Error('heartbeat unavailable');
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(0);
  });

  it('selects uncertain for invalid credentials after a possible send and defect before any send', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const { client: invalidClient } = newClient([serverFailure], 'a:b');
    expect(await create(invalidClient, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
    expect(await create(invalidClient, new AttemptContext())).toMatchObject({ branch: 'defect' });
  });

  it.each([new CredentialBrokerUnavailableError(), new Error('connection is not configured')])(
    'returns Retry and records notSent for a transient credential failure before any send: %s', async error => {
      const { client: unavailable } = newClient([created], undefined, error);
      const { client, requests } = newClient([created]);
      const context = new AttemptContext();
      expect(await create(unavailable, context)).toBeInstanceOf(RetryError);
      expect(context.marker).toMatchObject({ state: 'notSent' });
      context.attempt = 2;
      expect(await create(client, context)).toMatchObject({ branch: 'created' });
      expect(context.marker).toMatchObject({ sendCount: 1 });
      expect(requests).toHaveLength(1);
    });

  it('keeps the claim for a transient credential failure after a possible send', async () => {
    const { client, requests } = newClient([serverFailure]);
    const { client: unavailable } = newClient([serverFailure], undefined, new CredentialBrokerUnavailableError());
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await create(unavailable, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed' });
    expect(requests).toHaveLength(1);
  });

  it.each([
    new ReauthorizationRequiredError(), new ObjectNotFoundError(),
    new ConnectionNotSelectedError(), new CredentialBrokerRejectedError(),
    new CredentialMaterialError('credential application_password is required'), new TypeError('project credential decoding failed'),
  ])('selects defect before any send and uncertain after one for a terminal credential failure: %s', async error => {
    const { client, requests } = newClient([serverFailure]);
    const { client: terminal } = newClient([serverFailure], undefined, error);
    expect(await create(terminal, new AttemptContext())).toMatchObject({ branch: 'defect' });
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await create(terminal, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });
});
