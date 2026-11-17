// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { TwilioClient } from '../src/client.js';
import { SendMessageDefinition } from '../src/generated/definitions.js';
import type { Message } from '../src/generated/models.js';

const accountSid = 'AC00000000000000000000000000000000';
const messageSid = 'SMfedcba9876543210fedcba9876543210';
const callId = 'a29894ac-874e-5a65-a5a5-161c71200769';
const sendCap = SendMessageDefinition.maxSends!;

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readError: Error | undefined;
  readonly runId = 'run';
  readonly flowId = 'twilio-send-flow';
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

interface TwilioAnswer { status: number; body: string }

const created: TwilioAnswer = { status: 201, body: JSON.stringify({ sid: messageSid, account_sid: accountSid, from: '+14155550100', to: '+14155550123', status: 'queued' }) };
const serverFailure: TwilioAnswer = { status: 500, body: '{"code":20500}' };

function newClient(answers: TwilioAnswer[], authToken = 'auth-token-sentinel-0123456789ab') {
  const requests: ProviderRequest[] = [];
  const resolve = async () => ({ authMethodId: 'auth-token', authToken: new SecretString(authToken) });
  const client = new TwilioClient({ accountSid, defaultSender: '+14155550100' }, { resolve }, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body } = answers[Math.min(requests.length - 1, answers.length - 1)]!;
      return { status, headers: {}, body: Buffer.from(body) };
    },
  });
  return { client, requests };
}

async function send(client: TwilioClient, context: AttemptContext, maxSendsOverride?: number): Promise<MutationResult<Message> | RetryError> {
  const call = { id: callId, connection: { provider: 'twilio', name: 'twilio-messaging' }, operation: SendMessageDefinition.operation, context, ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try { return await runMutation(client.sendMessage(), call, { to: '+14155550123', body: 'hello' }); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

describe('Twilio bounded resend of unconfirmed messages', () => {
  it.each([500, 502, 408, 409, 425])('resends an unknown HTTP %i outcome below the send cap', async status => {
    const { client, requests } = newClient([{ status, body: '{}' }, created]);
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed', sendCount: 1 });
    context.attempt = 2;
    expect(await send(client, context)).toMatchObject({ branch: 'accepted' });
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

  it('reads back a saved message SID instead of resending', async () => {
    const { client, requests } = newClient([created, { status: 200, body: created.body }]);
    const context = new AttemptContext();
    expect(await send(client, context)).toMatchObject({ branch: 'accepted' });
    context.attempt = 2;
    expect(await send(client, context)).toMatchObject({ branch: 'accepted' });
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET']);
  });

  it.each([400, 403, 404, 413, 422])('never resends a definite HTTP %i refusal', async status => {
    const { client, requests } = newClient([{ status, body: '{"code":21211}' }]);
    const context = new AttemptContext();
    expect(await send(client, context)).toMatchObject({ branch: 'providerRejected' });
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain for a refusal of a resend after a possible send', async () => {
    const { client, requests } = newClient([serverFailure, { status: 400, body: '{"code":21211}' }]);
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await send(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(2);
  });

  it('returns Retry when the dispatch checkpoint is unreadable', async () => {
    const { client, requests } = newClient([created]);
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
    const { client: invalidClient } = newClient([serverFailure], '');
    expect(await send(invalidClient, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(1);
    expect(await send(invalidClient, new AttemptContext())).toMatchObject({ branch: 'defect' });
  });
});
