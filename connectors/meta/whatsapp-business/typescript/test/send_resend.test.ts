// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it, vi } from 'vitest';
import { DispatchNotAllowedError, Fence, InsufficientTimeError } from '@superdurable/connectors-sdk/singledispatch';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { WhatsAppBusinessClient } from '../src/client.js';
import { SendTextMessageDefinition } from '../src/generated/definitions.js';
import { CredentialMaterialError, decodeCredentials, type SentMessage } from '../src/generated/models.js';

/** Return the error the SDK project provider throws when the generated decoder rejects stored material. */
function sdkWrappedMaterialError(): TypeError {
  try { decodeCredentials({ unexpected_field: 'value' }); }
  catch { return new TypeError('project credential decoding failed'); }
  throw new Error('malformed stored material decoded');
}

const callId = 'dafd56f7-07e7-5223-956d-91714d91663a';
const sendCap = SendTextMessageDefinition.maxSends!;

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readError: Error | undefined;
  readonly runId = 'run';
  readonly flowId = 'whatsapp-send-flow';
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

interface MetaAnswer { status: number; body: string; bodyError?: 'transport' | 'stalled' | 'tooLarge' }

const sent: MetaAnswer = { status: 200, body: '{"messaging_product":"whatsapp","messages":[{"id":"wamid.example-text"}]}' };
const serverFailure: MetaAnswer = { status: 500, body: '{"error":{"message":"unknown"}}' };

function newClient(answers: MetaAnswer[], token = 'secret-meta-system-user-token', maxResponseBytes?: number, credentialError?: Error) {
  const requests: ProviderRequest[] = [];
  const resolve = async () => {
    if (credentialError) throw credentialError;
    return { accessToken: new SecretString(token) };
  };
  const config = { baseUrl: 'http://127.0.0.1:12345/v25.0', ...(maxResponseBytes === undefined ? {} : { maxResponseBytes }) };
  const client = new WhatsAppBusinessClient(config as never, { resolve }, {
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

async function send(client: WhatsAppBusinessClient, context: AttemptContext, maxSendsOverride?: number): Promise<MutationResult<SentMessage> | RetryError> {
  const call = { id: callId, connection: { provider: 'whatsapp-business', name: 'whatsapp-business' }, operation: SendTextMessageDefinition.operation, context, ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try { return await runMutation(client.sendTextMessage(), call, { phoneNumberId: '1906385232743451', recipient: '16315551234', text: 'Hello' }); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

describe('WhatsApp bounded resend of unconfirmed messages', () => {
  it.each([[500, ''], [502, ''], [408, ''], [409, ''], [425, ''], [418, ''], [400, '{"error":{"code":131000,"is_transient":true}}']])('resends an unknown HTTP %i outcome below the send cap', async (status, body) => {
    const { client, requests } = newClient([{ status, body }, sent]);
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

  it.each([
    ['DispatchNotAllowedError', () => new DispatchNotAllowedError()],
    ['the Fence AggregateError', () => new AggregateError([new InsufficientTimeError(), new Error('notSent write failed')])],
  ])('returns Retry when the Fence refuses with %s before sending', async (_name, refusal) => {
    const { client, requests } = newClient([sent]);
    const dispatch = vi.spyOn(Fence.prototype, 'dispatch').mockRejectedValue(refusal());
    // A cap of 1 makes an unknown outcome select uncertain, so Retry proves nothing was counted as sent.
    try { expect(await send(client, new AttemptContext(), 1)).toBeInstanceOf(RetryError); }
    finally { dispatch.mockRestore(); }
    expect(requests).toHaveLength(0);
  });

  it('never resends when a Step sets the send cap to 1', async () => {
    const { client, requests } = newClient([serverFailure]);
    expect(await send(client, new AttemptContext(), 1)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it.each([400, 401, 403, 404, 413, 422])('never resends a definite HTTP %i refusal', async status => {
    const { client, requests } = newClient([{ status, body: '{"error":{"code":131026}}' }]);
    const context = new AttemptContext();
    expect(await send(client, context)).toMatchObject({ branch: 'providerRejected' });
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it('treats a transient Graph error as unknown and selects uncertain at the send cap', async () => {
    const { client, requests } = newClient([{ status: 400, body: '{"error":{"code":131000,"is_transient":true}}' }]);
    const context = new AttemptContext();
    for (let attempt = 1; attempt < sendCap; attempt++) {
      context.attempt = attempt;
      expect(await send(client, context)).toBeInstanceOf(RetryError);
      expect(context.marker).toMatchObject({ state: 'claimed' });
    }
    context.attempt = sendCap;
    expect(await send(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(3);
  });

  it.each([
    [{ status: 200, body: '{}' }, undefined],
    [{ status: 200, body: 'not JSON' }, undefined],
    [{ status: 200, body: `{"messaging_product":"whatsapp","messages":[{"id":"wamid.${'x'.repeat(200)}"}]}` }, 128],
    [{ status: 200, body: '', bodyError: 'transport' }, undefined],
    [{ status: 200, body: '', bodyError: 'stalled' }, undefined],
    [{ status: 200, body: '', bodyError: 'tooLarge' }, undefined],
  ] as Array<[MetaAnswer, number | undefined]>)('never resends an accepted message whose response is unreadable: %j', async (answer, maxResponseBytes) => {
    const { client, requests } = newClient([answer], 'secret-meta-system-user-token', maxResponseBytes);
    expect(await send(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it.each([
    [{ status: 400, body: '', bodyError: 'transport' }, undefined],
    [{ status: 400, body: `{"error":{"code":100,"message":"${'x'.repeat(200)}"}}` }, 128],
  ] as Array<[MetaAnswer, number | undefined]>)('treats an unreadable client error as unknown: %j', async (answer, maxResponseBytes) => {
    const { client, requests } = newClient([answer, sent], 'secret-meta-system-user-token', maxResponseBytes);
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed' });
    context.attempt = 2;
    expect(await send(client, context)).toMatchObject({ branch: 'sent' });
    expect(requests).toHaveLength(2);
  });

  it('selects uncertain for an unlisted client error that reports a message ID', async () => {
    const { client, requests } = newClient([{ status: 409, body: sent.body }]);
    expect(await send(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain for a refusal after a rate-limited resend', async () => {
    const { client, requests } = newClient([serverFailure, { status: 429, body: '' }, { status: 400, body: '{"error":{"code":100}}' }]);
    const context = new AttemptContext();
    for (const attempt of [1, 2]) {
      context.attempt = attempt;
      expect(await send(client, context)).toBeInstanceOf(RetryError);
    }
    context.attempt = 3;
    expect(await send(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(3);
  });

  it('selects uncertain for a refusal of a resend after a possible send', async () => {
    const { client, requests } = newClient([serverFailure, { status: 400, body: '{"error":{"code":100}}' }]);
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await send(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(2);
  });

  it('never resends an incomplete acceptance', async () => {
    const { client, requests } = newClient([{ status: 200, body: '{"messages":[{"id":"wamid.example-text"}]}' }]);
    expect(await send(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('completes from a checkpointed message ID without resending', async () => {
    const { client, requests } = newClient([sent]);
    const context = new AttemptContext();
    expect(await send(client, context)).toMatchObject({ branch: 'sent' });
    context.attempt = 2;
    expect(await send(client, context)).toMatchObject({ branch: 'sent', value: { messageId: 'wamid.example-text' } });
    expect(requests).toHaveLength(1);
  });

  it('returns Retry when the dispatch checkpoint is unreadable', async () => {
    const { client, requests } = newClient([sent]);
    const context = new AttemptContext();
    context.readError = new Error('heartbeat unavailable');
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(0);
  });

  it('selects uncertain for invalid credentials after a possible send and defect before any send', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const { client: invalidClient } = newClient([serverFailure], 'bad\ntoken');
    expect(await send(invalidClient, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
    expect(await send(invalidClient, new AttemptContext())).toMatchObject({ branch: 'defect' });
  });

  it.each([new CredentialBrokerUnavailableError(), new Error('project store read failed'), new Error('connection is not configured'), new TypeError('fetch failed')])('retries a transient credential failure before any send without counting a send: %s', async credentialError => {
    const { client: unavailableClient, requests } = newClient([sent], undefined, undefined, credentialError);
    const context = new AttemptContext();
    expect(await send(unavailableClient, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(0);
    expect(context.marker).toMatchObject({ state: 'notSent' });
    context.attempt = 2;
    const { client, requests: laterRequests } = newClient([sent]);
    expect(await send(client, context)).toMatchObject({ branch: 'sent' });
    expect(context.marker).toMatchObject({ sendCount: 1 });
    expect(laterRequests).toHaveLength(1);
  });

  it('selects uncertain for malformed stored credentials after a possible send', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const { client: malformedClient } = newClient([serverFailure], undefined, undefined, new CredentialMaterialError('WhatsApp Business system-user token is unavailable'));
    expect(await send(malformedClient, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it.each([
    [new CredentialBrokerUnavailableError(), 'retry'], [new TypeError('fetch failed'), 'retry'],
    [new ObjectNotFoundError(), 'branch'], [sdkWrappedMaterialError(), 'branch'],
  ] as const)('listPhoneNumbers retries a transient credential failure and selects defect for a terminal one: %s', async (credentialError, kind) => {
    const { client, requests } = newClient([serverFailure], undefined, undefined, credentialError);
    const call = { id: callId, connection: { provider: 'whatsapp-business', name: 'whatsapp-business' }, operation: { connectorId: 'whatsapp-business', operationId: 'listPhoneNumbers' }, context: new AttemptContext() } as unknown as Call;
    const attempt = await client.listPhoneNumbers().invoke(call, { businessAccountId: '1234567890' });
    expect(attempt.kind).toBe(kind);
    if (kind === 'branch') expect(attempt).toMatchObject({ branch: 'defect' });
    expect(requests).toHaveLength(0);
  });

  it.each([new ReauthorizationRequiredError(), new ObjectNotFoundError(), new ConnectionNotSelectedError(), new CredentialBrokerRejectedError(), sdkWrappedMaterialError(), new CredentialMaterialError('WhatsApp Business system-user token is unavailable')])('selects defect for a terminal credential failure before any send: %s', async credentialError => {
    const { client, requests } = newClient([sent], undefined, undefined, credentialError);
    expect(await send(client, new AttemptContext())).toMatchObject({ branch: 'defect' });
    expect(requests).toHaveLength(0);
  });
});
