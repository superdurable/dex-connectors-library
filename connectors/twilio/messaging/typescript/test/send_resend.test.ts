// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it, vi } from 'vitest';
import { DispatchNotAllowedError, Fence, InsufficientTimeError } from '@superdurable/connectors-sdk/singledispatch';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
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

/** A Step deadline 1.2 seconds away, which shortens the Fence write window to about 0.7 seconds. */
class ShortDeadlineContext extends AttemptContext {
  override readonly deadline = new Date(Date.now() + 1_200);
}

interface TwilioAnswer { status: number; body: string; bodyError?: 'transport' | 'stalled' | 'tooLarge' }

const created: TwilioAnswer = { status: 201, body: JSON.stringify({ sid: messageSid, account_sid: accountSid, from: '+14155550100', to: '+14155550123', status: 'queued' }) };
const serverFailure: TwilioAnswer = { status: 500, body: '{"code":20500}' };

function newClient(answers: TwilioAnswer[] | ((request: ProviderRequest) => TwilioAnswer), authToken = 'auth-token-sentinel-0123456789ab') {
  const requests: ProviderRequest[] = [];
  const resolve = async () => ({ authMethodId: 'auth-token', authToken: new SecretString(authToken) });
  const client = new TwilioClient({ accountSid, defaultSender: '+14155550100' }, { resolve }, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body, bodyError } = typeof answers === 'function' ? answers(request) : answers[Math.min(requests.length - 1, answers.length - 1)]!;
      if (bodyError === 'transport') throw new ProviderTransportError(false, status, {});
      if (bodyError === 'stalled') throw new ProviderStalledError(status, {});
      if (bodyError === 'tooLarge') throw new BodyTooLargeError(status, {});
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

  it.each([
    ['DispatchNotAllowedError', () => new DispatchNotAllowedError()],
    ['the Fence AggregateError', () => new AggregateError([new InsufficientTimeError(), new Error('notSent write failed')])],
  ])('returns Retry when the Fence refuses with %s before sending', async (_name, refusal) => {
    const { client, requests } = newClient([created]);
    const dispatch = vi.spyOn(Fence.prototype, 'dispatch').mockRejectedValue(refusal());
    // A cap of 1 makes an unknown outcome select uncertain, so Retry proves nothing was counted as sent.
    try { expect(await send(client, new AttemptContext(), 1)).toBeInstanceOf(RetryError); }
    finally { dispatch.mockRestore(); }
    expect(requests).toHaveLength(0);
  });

  it('treats an AggregateError thrown after dispatch as an unknown outcome', async () => {
    const { client } = newClient([created]);
    const dispatch = vi.spyOn(Fence.prototype, 'dispatch').mockRejectedValue(new AggregateError([new Error('socket reset')]));
    try { expect(await send(client, new AttemptContext(), 1)).toMatchObject({ branch: 'uncertain' }); }
    finally { dispatch.mockRestore(); }
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

  it.each([
    { status: 201, body: '{"status":"queued"}' },
    { status: 201, body: '{"sid":"not-a-sid"}' },
    { status: 201, body: '', bodyError: 'transport' },
    { status: 201, body: '', bodyError: 'stalled' },
    { status: 201, body: '', bodyError: 'tooLarge' },
  ] as TwilioAnswer[])('never resends an accepted message whose response is unusable: %j', async answer => {
    const { client, requests } = newClient([answer]);
    expect(await send(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('reads back an error status that names a created message instead of resending', async () => {
    const { client, requests } = newClient(request => request.method === 'POST' ? { status: 409, body: created.body } : { status: 200, body: created.body });
    const context = new AttemptContext();
    expect(await send(client, context)).toMatchObject({ branch: 'accepted', value: { sid: messageSid } });
    expect(JSON.stringify(context.marker)).toContain(messageSid);
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET']);
  });

  it('selects uncertain when the named SID cannot be saved and its read-back is unavailable', async () => {
    const { client, requests } = newClient(request => request.method === 'POST' ? { status: 409, body: created.body } : { status: 503, body: '{}' });
    const context = new AttemptContext();
    const recordHeartbeat = context.recordHeartbeat.bind(context);
    context.recordHeartbeat = async (value: unknown) => {
      if (JSON.stringify(value).includes(messageSid)) throw new Error('heartbeat refused');
      await recordHeartbeat(value);
    };
    expect(await send(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET']);
  });

  it('retries a read-back of the saved named SID without resending', async () => {
    const { client, requests } = newClient(request => request.method === 'POST' ? { status: 409, body: created.body } : requests.length < 3 ? { status: 503, body: '{}' } : { status: 200, body: created.body });
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await send(client, context)).toMatchObject({ branch: 'accepted' });
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET', 'GET']);
  });

  it.each([
    { status: 400, body: '', bodyError: 'transport' },
    { status: 400, body: '', bodyError: 'tooLarge' },
  ] as TwilioAnswer[])('treats an unreadable refusal body as an unknown outcome: %j', async answer => {
    const { client, requests } = newClient([answer, created]);
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed' });
    context.attempt = 2;
    expect(await send(client, context)).toMatchObject({ branch: 'accepted' });
    expect(requests).toHaveLength(2);
  });

  it('reads back an accepted SID whose message does not decode instead of resending', async () => {
    const undecodable = JSON.stringify({ ...JSON.parse(created.body), status: 'Not A Status' });
    const { client, requests } = newClient(request => request.method === 'POST' ? { status: 201, body: undecodable } : { status: 200, body: created.body });
    const context = new AttemptContext();
    expect(await send(client, context)).toMatchObject({ branch: 'accepted', value: { sid: messageSid } });
    expect(JSON.stringify(context.marker)).toContain(messageSid);
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET']);
  });

  it.each([201, 409])('never saves or reads back a named SID that contains the credential after HTTP %i', async status => {
    const hexAuthToken = '0123456789abcdef0123456789abcdef';
    const reflected = JSON.stringify({ ...JSON.parse(created.body), sid: `SM${hexAuthToken}` });
    const { client, requests } = newClient([{ status, body: reflected }], hexAuthToken);
    const context = new AttemptContext();
    const result = await send(client, context);
    expect(result).toMatchObject({ branch: 'uncertain' });
    expect(JSON.stringify((result as MutationResult<Message>).receipt)).not.toContain(hexAuthToken);
    expect(JSON.stringify(context.marker)).not.toContain(hexAuthToken);
    expect(requests).toHaveLength(1);
  });

  it.each([
    { status: 401, body: '', bodyError: 'transport' },
    { status: 401, body: '', bodyError: 'tooLarge' },
  ] as TwilioAnswer[])('treats an unreadable 401 body as an unknown outcome: %j', async answer => {
    const { client, requests } = newClient([answer, { status: 401, body: '{"code":20003}' }]);
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed' });
    context.attempt = 2;
    expect(await send(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(2);
  });

  it('never resends when the write deadline cuts a body after 2xx headers', async () => {
    const requests: ProviderRequest[] = [];
    const resolve = async () => ({ authMethodId: 'auth-token', authToken: new SecretString('auth-token-sentinel-0123456789ab') });
    const client = new TwilioClient({ accountSid, defaultSender: '+14155550100' }, { resolve }, {
      send: (request: ProviderRequest) => {
        requests.push(request);
        // Headers arrived; the Fence write window then aborts the body read, as sendProviderRequest reports it.
        return new Promise((_, reject) => request.signal?.addEventListener('abort', () => reject(new ProviderTransportError(false, 201, {})), { once: true }));
      },
    });
    const context = new ShortDeadlineContext();
    expect(await send(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain for terminal credentials after a possible send and defect before any send', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    expect(await send(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const { client: invalidClient } = newClient([serverFailure], '');
    expect(await send(invalidClient, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
    expect(await send(invalidClient, new AttemptContext())).toMatchObject({ branch: 'defect' });
  });
});
