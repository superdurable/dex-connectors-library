// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import {
  ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError,
} from '@superdurable/connectors-sdk/projectconfig';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { HootsuiteClient } from '../src/client.js';
import { SchedulePostDefinition } from '../src/generated/definitions.js';
import type { ScheduledPostReceipt } from '../src/generated/models.js';

const callId = '58f21093-ca67-5b5a-b692-0443b2041e5f';
const sendCap = SchedulePostDefinition.maxSends!;
const connection = { provider: 'hootsuite', name: 'hootsuite' };

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readError: Error | undefined;
  readonly runId = 'run';
  readonly flowId = 'hootsuite-schedule-flow';
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

interface HootsuiteAnswer { status: number; body: string; bodyError?: 'transport' | 'stalled' | 'tooLarge' }

const scheduled: HootsuiteAnswer = { status: 200, body: '{"data":[{"id":"4138476039","state":"SCHEDULED","socialProfile":{"id":"115185509"}}]}' };
const serverFailure: HootsuiteAnswer = { status: 500, body: '{"errors":[{"message":"internal error"}]}' };

function newClient(answers: HootsuiteAnswer[], accessToken = 'test-hootsuite-token', resolveError?: Error) {
  const requests: ProviderRequest[] = [];
  const staticCredentials = new StaticCredentialProvider([[connection, {
    oauthClientId: 'test-client', oauthClientSecret: new SecretString('test-client-secret'),
    accessToken: new SecretString(accessToken), refreshToken: new SecretString('test-refresh-token'),
  }]]);
  const failingCredentials = { resolve: async () => { throw resolveError; } } as unknown as typeof staticCredentials;
  const credentials = resolveError ? failingCredentials : staticCredentials;
  const client = new HootsuiteClient({ baseUrl: 'http://127.0.0.1:12345' } as never, credentials, {
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

async function schedule(client: HootsuiteClient, context: AttemptContext, maxSendsOverride?: number): Promise<MutationResult<ScheduledPostReceipt> | RetryError> {
  const call = { id: callId, connection, operation: SchedulePostDefinition.operation, context, ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try { return await runMutation(client.schedulePost(), call, { text: 'A useful update', socialProfileId: '115185509', scheduledSendTime: '2026-11-15T14:00:00Z' }); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

describe('Hootsuite Query credential failures', () => {
  it.each([
    ['reauthorization', new ReauthorizationRequiredError(), true],
    ['missing connection', new ObjectNotFoundError(), true],
    ['unselected connection', new ConnectionNotSelectedError(), true],
    ['broker refusal', new CredentialBrokerRejectedError(), true],
    ['malformed stored material', new TypeError('project credential decoding failed'), true],
    ['broker outage', new CredentialBrokerUnavailableError(), false],
    ['unlisted error', new Error('unlisted'), false],
  ] as const)('retries a %s unless it is terminal, without a request', async (_name, error, isTerminal) => {
    const { client, requests } = newClient([serverFailure], undefined, error);
    const call = { id: callId, connection, operation: { connectorId: 'hootsuite', operationId: 'listSocialProfiles' }, context: new AttemptContext() } as unknown as Call;
    const attempt = await client.listSocialProfiles().invoke(call, {});
    expect(attempt.kind).toBe(isTerminal ? 'branch' : 'retry');
    if (attempt.kind === 'branch') expect(attempt.branch).toBe('defect');
    expect(requests).toHaveLength(0);
  });
});

describe('Hootsuite bounded resend of unconfirmed scheduled messages', () => {
  it.each([500, 502, 408, 409, 425, 418])('resends an unknown HTTP %i outcome below the send cap', async status => {
    const { client, requests } = newClient([{ status, body: '{"errors":[]}' }, scheduled]);
    const context = new AttemptContext();
    expect(await schedule(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed', sendCount: 1 });
    context.attempt = 2;
    expect(await schedule(client, context)).toMatchObject({ branch: 'scheduled' });
    expect(context.marker).toMatchObject({ sendCount: 2 });
    expect(requests).toHaveLength(2);
  });

  it('selects uncertain at the send cap and never sends again', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    for (let attempt = 1; attempt < sendCap; attempt++) {
      context.attempt = attempt;
      expect(await schedule(client, context)).toBeInstanceOf(RetryError);
    }
    context.attempt = sendCap;
    expect(await schedule(client, context)).toMatchObject({ branch: 'uncertain' });
    context.attempt++;
    expect(await schedule(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(3);
  });

  it('never resends when a Step sets the send cap to 1', async () => {
    const { client, requests } = newClient([serverFailure]);
    expect(await schedule(client, new AttemptContext(), 1)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it.each([
    { status: 200, body: 'not JSON' },
    { status: 200, body: '{"data":[]}' },
    { status: 200, body: '', bodyError: 'transport' },
    { status: 200, body: '', bodyError: 'stalled' },
    { status: 200, body: '', bodyError: 'tooLarge' },
  ] as HootsuiteAnswer[])('never resends an accepted request whose response is unreadable: %j', async answer => {
    const { client, requests } = newClient([answer, scheduled]);
    expect(await schedule(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('never resends an unlisted error response that reports a message ID', async () => {
    const { client, requests } = newClient([{ status: 409, body: scheduled.body }]);
    expect(await schedule(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('treats an unreadable refusal body as an unknown outcome', async () => {
    const { client, requests } = newClient([{ status: 400, body: '', bodyError: 'transport' }, scheduled]);
    const context = new AttemptContext();
    expect(await schedule(client, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed' });
    context.attempt = 2;
    expect(await schedule(client, context)).toMatchObject({ branch: 'scheduled' });
    expect(requests).toHaveLength(2);
  });

  it('keeps the claim when a credential refresh fails after a possible send', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    expect(await schedule(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const unavailable = new HootsuiteClient({ baseUrl: 'http://127.0.0.1:12345' } as never, new StaticCredentialProvider([]), {
      send: async (request: ProviderRequest) => { requests.push(request); return { status: 500, headers: {}, body: Buffer.from('{}') }; },
    });
    expect(await schedule(unavailable, context)).toBeInstanceOf(RetryError);
    expect(JSON.stringify(context.marker)).toContain('claimed');
    expect(requests).toHaveLength(1);
  });

  it.each([400, 401, 403, 404, 413, 422])('never resends a definite HTTP %i refusal', async status => {
    const { client, requests } = newClient([{ status, body: '{"errors":[{"code":1000}]}' }]);
    const context = new AttemptContext();
    expect(await schedule(client, context)).toMatchObject({ branch: 'providerRejected' });
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain for a refusal of a resend after a possible send', async () => {
    const { client, requests } = newClient([serverFailure, { status: 400, body: '{"errors":[{"code":1000}]}' }]);
    const context = new AttemptContext();
    expect(await schedule(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await schedule(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(2);
  });

  it('never resends an accepted but incomplete response', async () => {
    const { client, requests } = newClient([{ status: 200, body: '{"data":[{"id":"4138476039"}]}' }]);
    expect(await schedule(client, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('never resends a confirmed message', async () => {
    const { client, requests } = newClient([scheduled]);
    const context = new AttemptContext();
    expect(await schedule(client, context)).toMatchObject({ branch: 'scheduled' });
    context.attempt = 2;
    expect(await schedule(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });

  it('returns Retry when the dispatch checkpoint is unreadable', async () => {
    const { client, requests } = newClient([scheduled]);
    const context = new AttemptContext();
    context.readError = new Error('heartbeat unavailable');
    expect(await schedule(client, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(0);
  });

  it('selects uncertain for an invalid credential after a possible send and defect before any send', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    expect(await schedule(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const { client: invalidClient } = newClient([serverFailure], 'bad\ntoken');
    expect(await schedule(invalidClient, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
    expect(await schedule(invalidClient, new AttemptContext())).toMatchObject({ branch: 'defect' });
  });

  it.each([new CredentialBrokerUnavailableError(), new Error('connection is not configured')])(
    'returns Retry and records notSent for a transient credential failure before any send: %s', async error => {
      const { client: unavailable } = newClient([scheduled], undefined, error);
      const { client, requests } = newClient([scheduled]);
      const context = new AttemptContext();
      expect(await schedule(unavailable, context)).toBeInstanceOf(RetryError);
      expect(context.marker).toMatchObject({ state: 'notSent' });
      context.attempt = 2;
      expect(await schedule(client, context)).toMatchObject({ branch: 'scheduled' });
      expect(context.marker).toMatchObject({ sendCount: 1 });
      expect(requests).toHaveLength(1);
    });

  it.each([
    new ReauthorizationRequiredError(), new ObjectNotFoundError(),
    new ConnectionNotSelectedError(), new CredentialBrokerRejectedError(),
    // The SDK project provider replaces a malformed-material decoder error with this TypeError.
    new TypeError('project credential decoding failed'),
  ])('selects defect before any send and uncertain after one for a terminal credential failure: %s', async error => {
    const { client, requests } = newClient([serverFailure]);
    const { client: terminal } = newClient([serverFailure], undefined, error);
    expect(await schedule(terminal, new AttemptContext())).toMatchObject({ branch: 'defect' });
    const context = new AttemptContext();
    expect(await schedule(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await schedule(terminal, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(1);
  });
});
