// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it, vi } from 'vitest';
import { DispatchNotAllowedError, Fence } from '@superdurable/connectors-sdk/singledispatch';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import {
  ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError,
} from '@superdurable/connectors-sdk/projectconfig';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { TelegramClient } from '../src/client.js';
import { SendMessageDefinition } from '../src/generated/definitions.js';
import { CredentialMaterialError, type SentMessage } from '../src/generated/models.js';

const callId = 'bbe66292-151f-5c40-bdfe-0553b4fb902f';
const sendCap = SendMessageDefinition.maxSends!;

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readError: Error | undefined;
  readonly runId = 'run';
  readonly flowId = 'telegram-send-flow';
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

interface TelegramAnswer { status: number; body: string; bodyError?: 'transport' | 'stalled' | 'tooLarge' }

const sent: TelegramAnswer = { status: 200, body: '{"ok":true,"result":{"message_id":52,"date":1794110000,"chat":{"id":-1001234567890}}}' };
const serverFailure: TelegramAnswer = { status: 500, body: '{"ok":false,"error_code":500}' };

function newClient(answers: TelegramAnswer[], token = '123456:fake_test_token', resolveError?: Error) {
  const requests: ProviderRequest[] = [];
  const resolve = async () => {
    if (resolveError) throw resolveError;
    return { botToken: new SecretString(token) };
  };
  const client = new TelegramClient({ baseUrl: 'http://127.0.0.1:12345' } as never, { resolve }, {
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

async function send(client: TelegramClient, context: AttemptContext, maxSendsOverride?: number): Promise<MutationResult<SentMessage> | RetryError> {
  const call = { id: callId, connection: { provider: 'telegram', name: 'telegram' }, operation: SendMessageDefinition.operation, context, ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try { return await runMutation(client.sendMessage(), call, { chatId: '@meridian_team', text: 'hello' }); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

describe('Telegram bounded resend of unconfirmed messages', () => {
  it.each([500, 502, 408, 409, 425, 418])('resends an unknown HTTP %i outcome below the send cap', async status => {
    const { client, requests } = newClient([{ status, body: '{"ok":false}' }, sent]);
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed', sendCount: 1 });
    context.attempt = 2;
    expect(await send(client, context)).toMatchObject({ branch: 'sent' });
    expect(context.marker).toMatchObject({ sendCount: 2 });
    expect(requests).toHaveLength(2);
  });

  it('selects uncertain at the send cap and never sends again', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    for (let attempt = 1; attempt < sendCap; attempt++) {
      context.attempt = attempt;
      expect(await send(client, context)).toBeInstanceOf(RetryError);
    }
    context.attempt = sendCap;
    expect(await send(client, context)).toMatchObject({ branch: 'uncertain' });
    context.attempt++;
    expect(await send(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(3);
  });

  it('never resends when a Step sets the send cap to 1', async () => {
    const { client, requests } = newClient([serverFailure]);
    expect(await send(client, new AttemptContext(), 1)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it.each([
    { status: 200, body: 'not JSON' },
    { status: 200, body: '{}' },
    { status: 200, body: '', bodyError: 'transport' },
    { status: 200, body: '', bodyError: 'stalled' },
    { status: 200, body: '', bodyError: 'tooLarge' },
  ] as TelegramAnswer[])('never resends an accepted message whose response is unreadable: %j', async answer => {
    const { client, requests } = newClient([answer, sent]);
    expect(await send(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('never resends an unlisted error response that reports acceptance', async () => {
    const { client, requests } = newClient([{ status: 409, body: sent.body }]);
    expect(await send(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('treats an unreadable refusal body as an unknown outcome', async () => {
    const { client, requests } = newClient([{ status: 400, body: '', bodyError: 'transport' }, sent]);
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed' });
    context.attempt = 2;
    expect(await send(client, context)).toMatchObject({ branch: 'sent' });
    expect(requests).toHaveLength(2);
  });

  it.each([400, 401, 403, 404, 413, 422])('never resends a definite HTTP %i refusal', async status => {
    const { client, requests } = newClient([{ status, body: `{"ok":false,"error_code":${status}}` }]);
    const context = new AttemptContext();
    expect(await send(client, context)).toMatchObject({ branch: 'providerRejected' });
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain for a refusal of a resend after a possible send', async () => {
    const { client, requests } = newClient([serverFailure, { status: 400, body: '{"ok":false,"error_code":400}' }]);
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await send(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(2);
  });

  it('never resends an accepted but incomplete response', async () => {
    const { client, requests } = newClient([{ status: 200, body: '{"ok":true,"result":{"message_id":52}}' }]);
    expect(await send(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('never resends a confirmed message', async () => {
    const { client, requests } = newClient([sent]);
    const context = new AttemptContext();
    expect(await send(client, context)).toMatchObject({ branch: 'sent' });
    context.attempt = 2;
    expect(await send(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('returns Retry when the Fence refuses before sending', async () => {
    const { client, requests } = newClient([sent]);
    class ShortWindowContext extends AttemptContext {
      override readonly deadline = new Date(Date.now() + 50);
      override async recordHeartbeat(): Promise<void> { throw new Error('heartbeat write failed'); }
    }
    // A cap of 1 makes an unknown outcome select uncertain, so Retry proves nothing was counted as sent.
    expect(await send(client, new ShortWindowContext(), 1)).toBeInstanceOf(RetryError);
    const refusal = vi.spyOn(Fence.prototype, 'dispatch').mockRejectedValue(new DispatchNotAllowedError());
    try { expect(await send(client, new AttemptContext(), 1)).toBeInstanceOf(RetryError); }
    finally { refusal.mockRestore(); }
    expect(requests).toHaveLength(0);
  });

  it('getChat retries a transient credential failure and selects defect only for a terminal one', async () => {
    const call = { id: callId, connection: { provider: 'telegram', name: 'telegram' }, operation: { connectorId: 'telegram', operationId: 'getChat' }, context: new AttemptContext() } as unknown as Call;
    for (const error of [new CredentialBrokerUnavailableError(), new Error('connection is not configured')]) {
      const { client, requests } = newClient([sent], undefined, error);
      expect((await client.getChat().invoke(call, { chatId: '@meridian_team' })).kind).toBe('retry');
      expect(requests).toHaveLength(0);
    }
    for (const error of [new ReauthorizationRequiredError(), new ObjectNotFoundError(), new ConnectionNotSelectedError(), new CredentialBrokerRejectedError(), new CredentialMaterialError('bot token is required')]) {
      const { client } = newClient([sent], undefined, error);
      expect(await client.getChat().invoke(call, { chatId: '@meridian_team' })).toMatchObject({ kind: 'branch', branch: 'defect' });
    }
  });

  it('returns Retry when the dispatch checkpoint is unreadable', async () => {
    const { client, requests } = newClient([sent]);
    const context = new AttemptContext();
    context.readError = new Error('heartbeat unavailable');
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(0);
  });

  it('selects uncertain for an invalid token after a possible send and defect before any send', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const { client: invalidClient } = newClient([serverFailure], 'bad');
    expect(await send(invalidClient, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
    expect(await send(invalidClient, new AttemptContext())).toMatchObject({ branch: 'defect' });
  });

  it.each([new CredentialBrokerUnavailableError(), new Error('connection is not configured')])(
    'returns Retry and records notSent for a transient credential failure before any send: %s', async error => {
      const { client: unavailable } = newClient([sent], undefined, error);
      const { client, requests } = newClient([sent]);
      const context = new AttemptContext();
      expect(await send(unavailable, context)).toBeInstanceOf(RetryError);
      expect(context.marker).toMatchObject({ state: 'notSent' });
      context.attempt = 2;
      expect(await send(client, context)).toMatchObject({ branch: 'sent' });
      expect(context.marker).toMatchObject({ sendCount: 1 });
      expect(requests).toHaveLength(1);
    });

  it('keeps the claim for a transient credential failure after a possible send', async () => {
    const { client, requests } = newClient([serverFailure]);
    const { client: unavailable } = newClient([serverFailure], undefined, new CredentialBrokerUnavailableError());
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await send(unavailable, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed' });
    expect(requests).toHaveLength(1);
  });

  it.each([
    new ReauthorizationRequiredError(), new ObjectNotFoundError(),
    new ConnectionNotSelectedError(), new CredentialBrokerRejectedError(),
    new CredentialMaterialError('credential bot_token is required'), new TypeError('project credential decoding failed'),
  ])('selects defect before any send and uncertain after one for a terminal credential failure: %s', async error => {
    const { client, requests } = newClient([serverFailure]);
    const { client: terminal } = newClient([serverFailure], undefined, error);
    expect(await send(terminal, new AttemptContext())).toMatchObject({ branch: 'defect' });
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await send(terminal, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });
});
