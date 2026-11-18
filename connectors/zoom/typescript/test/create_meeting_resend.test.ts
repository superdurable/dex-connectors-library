// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { ZoomClient } from '../src/client.js';
import { CreateMeetingDefinition } from '../src/generated/definitions.js';
import type { CreateMeetingInput, Meeting } from '../src/generated/models.js';

const callId = 'd095d4cd-8f7d-53db-926e-50307d935d1f';
const sendCap = CreateMeetingDefinition.maxSends!;
const input: CreateMeetingInput = { topic: 'Consultation', startTime: '2036-02-24T09:00:00-08:00', timeZone: 'America/Los_Angeles', durationMinutes: 30 } as CreateMeetingInput;

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readError: Error | undefined;
  readonly runId = 'run';
  readonly flowId = 'zoom-create-flow';
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

interface ZoomAnswer { status: number; body: string; bodyError?: 'transport' | 'stalled' | 'tooLarge' }

const meetingBody = '{"id":92674392836,"topic":"Consultation","type":2,"start_time":"2036-02-24T17:00:00Z","duration":30,"timezone":"America/Los_Angeles"}';
const created: ZoomAnswer = { status: 201, body: meetingBody };
const serverFailure: ZoomAnswer = { status: 500, body: '{"code":500,"message":"Unknown"}' };

function newClient(answers: ZoomAnswer[] | ((request: ProviderRequest) => ZoomAnswer), hasCredentials = true, accessToken = 'zoom-test-access-token', maxResponseBytes?: number) {
  const requests: ProviderRequest[] = [];
  const credentials = { accessToken: new SecretString(accessToken), oauthClientId: 'client-id',
    oauthClientSecret: new SecretString('client-secret'), refreshToken: new SecretString('refresh-token') };
  const resolve = async () => {
    if (!hasCredentials) throw new Error('connection credentials are unavailable');
    return credentials;
  };
  const config = { endpoint: 'http://127.0.0.1:12345/v2', ...(maxResponseBytes === undefined ? {} : { maxResponseBytes }) };
  const client = new ZoomClient(config as never, { resolve, resolveWithRefresh: resolve } as never, {
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

async function create(client: ZoomClient, context: AttemptContext, maxSendsOverride?: number): Promise<MutationResult<Meeting> | RetryError> {
  const call = { id: callId, connection: { provider: 'zoom', name: 'zoom-test' }, operation: CreateMeetingDefinition.operation, context, ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try { return await runMutation(client.createMeeting(), call, input); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

describe('Zoom bounded resend of unconfirmed meeting creates', () => {
  it.each([500, 502, 408, 409, 425, 418, 307])('resends an unknown HTTP %i outcome below the send cap', async status => {
    const { client, requests } = newClient([{ status, body: `{"code":${status}}` }, created]);
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

  it('reads back a saved meeting ID instead of resending', async () => {
    const { client, requests } = newClient([created, { status: 200, body: meetingBody }]);
    const context = new AttemptContext();
    expect(await create(client, context)).toMatchObject({ branch: 'created' });
    context.attempt = 2;
    expect(await create(client, context)).toMatchObject({ branch: 'created' });
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET']);
  });

  it.each([400, 403, 404, 422])('never resends a definite HTTP %i refusal', async status => {
    const { client, requests } = newClient([{ status, body: '{"code":300}' }]);
    const context = new AttemptContext();
    expect(await create(client, context)).toMatchObject({ branch: 'providerRejected' });
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain for a refusal of a resend after a possible send', async () => {
    const { client, requests } = newClient([serverFailure, { status: 400, body: '{"code":300}' }]);
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await create(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(2);
  });

  it('never resends an accepted meeting ID with an unusable body', async () => {
    const { client, requests } = newClient([{ status: 201, body: '{"id":92674392836,"start_time":"not a time"}' }]);
    expect(await create(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it.each([
    [{ status: 201, body: '{"id":"not-a-number"}' }, undefined],
    [{ status: 201, body: '{}' }, undefined],
    [{ status: 201, body: `{"agenda":"${'x'.repeat(4096)}"}` }, 2048],
    [{ status: 201, body: '', bodyError: 'transport' }, undefined],
    [{ status: 201, body: '', bodyError: 'stalled' }, undefined],
    [{ status: 201, body: '', bodyError: 'tooLarge' }, undefined],
  ] as Array<[ZoomAnswer, number | undefined]>)('never resends an accepted create whose response is unusable: %j', async (answer, maxResponseBytes) => {
    const { client, requests } = newClient([answer], true, 'zoom-test-access-token', maxResponseBytes);
    expect(await create(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('reads back a client error that names a created meeting instead of resending', async () => {
    const { client, requests } = newClient(request => request.method === 'POST' ? { status: 425, body: '{"id":92674392836}' } : { status: 200, body: meetingBody });
    const context = new AttemptContext();
    expect(await create(client, context)).toMatchObject({ branch: 'created', value: { id: 92674392836 } });
    expect(JSON.stringify(context.marker)).toContain('92674392836');
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET']);
  });

  it('selects uncertain when the named meeting ID cannot be saved and its read-back is unavailable', async () => {
    const { client, requests } = newClient(request => request.method === 'POST' ? { status: 425, body: '{"id":92674392836}' } : { status: 503, body: '{}' });
    const context = new AttemptContext();
    const recordHeartbeat = context.recordHeartbeat.bind(context);
    context.recordHeartbeat = async (value: unknown) => {
      if (JSON.stringify(value).includes('92674392836')) throw new Error('heartbeat refused');
      await recordHeartbeat(value);
    };
    expect(await create(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET']);
  });

  it('retries a read-back of the saved named meeting ID without resending', async () => {
    const { client, requests } = newClient(request => request.method === 'POST' ? { status: 425, body: '{"id":92674392836}' } : requests.length < 3 ? { status: 503, body: '{}' } : { status: 200, body: meetingBody });
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await create(client, context)).toMatchObject({ branch: 'created' });
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET', 'GET']);
  });

  it('retries an unlisted client error with an unknown-outcome failure kind', async () => {
    const { client } = newClient([{ status: 425, body: '{"code":425}' }]);
    const result = await create(client, new AttemptContext());
    expect(result).toBeInstanceOf(RetryError);
    expect((result as RetryError).failure.kind).toBe('AVAILABILITY');
  });

  it.each([
    { status: 400, body: '', bodyError: 'transport' },
    { status: 400, body: '', bodyError: 'tooLarge' },
  ] as ZoomAnswer[])('treats an unreadable refusal body as an unknown outcome: %j', async answer => {
    const { client, requests } = newClient([answer, created]);
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed' });
    context.attempt = 2;
    expect(await create(client, context)).toMatchObject({ branch: 'created' });
    expect(requests).toHaveLength(2);
  });

  it('selects uncertain for invalid credentials after a possible send', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const { client: invalidClient } = newClient([serverFailure], true, '');
    expect(await create(invalidClient, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('returns Retry when the dispatch checkpoint is unreadable', async () => {
    const { client, requests } = newClient([created]);
    const context = new AttemptContext();
    context.readError = new Error('heartbeat unavailable');
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(0);
  });

  it.each([
    { status: 401, body: '', bodyError: 'transport' },
    { status: 401, body: '', bodyError: 'tooLarge' },
  ] as ZoomAnswer[])('treats an unreadable 401 body as an unknown outcome: %j', async answer => {
    const { client, requests } = newClient([answer, { status: 401, body: '{"code":124,"message":"Invalid access token"}' }]);
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed' });
    context.attempt = 2;
    expect(await create(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(2);
  });

  it('never resends when the write deadline cuts a body after 2xx headers', async () => {
    const requests: ProviderRequest[] = [];
    const credentials = { accessToken: new SecretString('zoom-test-access-token'), oauthClientId: 'client-id',
      oauthClientSecret: new SecretString('client-secret'), refreshToken: new SecretString('refresh-token') };
    const resolve = async () => credentials;
    const client = new ZoomClient({ endpoint: 'http://127.0.0.1:12345/v2' } as never, { resolve, resolveWithRefresh: resolve } as never, {
      send: (request: ProviderRequest) => {
        requests.push(request);
        // Headers arrived; the Fence write window then aborts the body read, as sendProviderRequest reports it.
        return new Promise((_, reject) => request.signal?.addEventListener('abort', () => reject(new ProviderTransportError(false, 201, {})), { once: true }));
      },
    });
    const context = new AttemptContext();
    // A Step deadline 1.2 seconds away shortens the Fence write window to about 0.7 seconds.
    (context as { deadline: Date }).deadline = new Date(Date.now() + 1_200);
    expect(await create(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('retries without sending when the window is short and notSent cannot be recorded at the send cap', async () => {
    const { client, requests } = newClient([serverFailure, serverFailure, { status: 429, body: '{"code":429}' }]);
    const context = new AttemptContext();
    for (let attempt = 1; attempt <= 3; attempt++) {
      context.attempt = attempt;
      expect(await create(client, context)).toBeInstanceOf(RetryError);
    }
    expect(context.marker).toMatchObject({ state: 'canResend', sendCount: 2 });
    context.attempt = 4;
    (context as { deadline: Date }).deadline = new Date(0);
    context.recordHeartbeat = async () => { throw new Error('heartbeat unavailable'); };
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(3);
  });

  it('retries a credential failure after a possible send', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    expect(await create(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const { client: failingClient } = newClient([serverFailure], false);
    expect(await create(failingClient, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(1);
  });

  it.each([
    { error: new ReauthorizationRequiredError(), terminal: true },
    { error: new ObjectNotFoundError(), terminal: true },
    { error: new ConnectionNotSelectedError(), terminal: true },
    { error: new CredentialBrokerRejectedError(), terminal: true },
    { error: new CredentialBrokerUnavailableError(), terminal: false },
    { error: new Error('connection is not configured'), terminal: false },
    { error: new TypeError('fetch failed'), terminal: false },
  ])('classifies a credential failure before any send: $error.name', async ({ error, terminal }) => {
    const resolve = async () => { throw error; };
    const client = new ZoomClient({ endpoint: 'http://127.0.0.1:12345/v2' } as never, { resolve, resolveWithRefresh: resolve } as never, {
      send: async () => { throw new Error('a call without credentials must not reach Zoom'); },
    });
    const result = await create(client, new AttemptContext());
    if (terminal) expect(result).toMatchObject({ branch: 'defect' });
    else expect(result).toBeInstanceOf(RetryError);
  });
});
