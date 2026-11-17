// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { WhatsAppBusinessClient } from '../src/client.js';
import { SendTextMessageDefinition } from '../src/generated/definitions.js';
import type { SentMessage } from '../src/generated/models.js';

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

interface MetaAnswer { status: number; body: string }

const sent: MetaAnswer = { status: 200, body: '{"messaging_product":"whatsapp","messages":[{"id":"wamid.example-text"}]}' };
const serverFailure: MetaAnswer = { status: 500, body: '{"error":{"message":"unknown"}}' };

function newClient(answers: MetaAnswer[], token = 'secret-meta-system-user-token') {
  const requests: ProviderRequest[] = [];
  const resolve = async () => ({ accessToken: new SecretString(token) });
  const client = new WhatsAppBusinessClient({ baseUrl: 'http://127.0.0.1:12345/v25.0' } as never, { resolve }, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body } = answers[Math.min(requests.length - 1, answers.length - 1)]!;
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
  it.each([[500, ''], [502, ''], [408, ''], [409, ''], [425, ''], [418, ''], [200, '{}']])('resends an unknown HTTP %i outcome below the send cap', async (status, body) => {
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

  it('retries a credential failure after a possible send and selects defect before any send', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const { client: invalidClient } = newClient([serverFailure], 'bad\ntoken');
    expect(await send(invalidClient, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(1);
    expect(await send(invalidClient, new AttemptContext())).toMatchObject({ branch: 'defect' });
  });
});
