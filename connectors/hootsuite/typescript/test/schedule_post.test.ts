// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
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

interface HootsuiteAnswer { status: number; body: string }

const scheduled: HootsuiteAnswer = { status: 200, body: '{"data":[{"id":"4138476039","state":"SCHEDULED","socialProfile":{"id":"115185509"}}]}' };
const serverFailure: HootsuiteAnswer = { status: 500, body: '{"errors":[{"message":"internal error"}]}' };

function newClient(answers: HootsuiteAnswer[], accessToken = 'test-hootsuite-token') {
  const requests: ProviderRequest[] = [];
  const credentials = new StaticCredentialProvider([[connection, {
    oauthClientId: 'test-client', oauthClientSecret: new SecretString('test-client-secret'),
    accessToken: new SecretString(accessToken), refreshToken: new SecretString('test-refresh-token'),
  }]]);
  const client = new HootsuiteClient({ baseUrl: 'http://127.0.0.1:12345' } as never, credentials, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body } = answers[Math.min(requests.length - 1, answers.length - 1)]!;
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

  it('resends a malformed success response below the send cap', async () => {
    const { client } = newClient([{ status: 200, body: 'not JSON' }, scheduled]);
    const context = new AttemptContext();
    expect(await schedule(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await schedule(client, context)).toMatchObject({ branch: 'scheduled' });
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

  it('retries a credential failure after a possible send and selects defect before any send', async () => {
    const { client, requests } = newClient([serverFailure]);
    const context = new AttemptContext();
    expect(await schedule(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const { client: invalidClient } = newClient([serverFailure], 'bad\ntoken');
    expect(await schedule(invalidClient, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(1);
    expect(await schedule(invalidClient, new AttemptContext())).toMatchObject({ branch: 'defect' });
  });
});
