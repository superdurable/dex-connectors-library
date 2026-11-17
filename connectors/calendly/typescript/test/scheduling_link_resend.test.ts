// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { CalendlyClient } from '../src/client.js';
import { CreateSchedulingLinkDefinition } from '../src/generated/definitions.js';
import type { Credentials, SchedulingLink } from '../src/generated/models.js';

const callId = '345b8fc6-bc68-513b-af1b-4f197c2e59f6';
const connection = { provider: 'calendly', name: 'scheduling' };
const eventTypeUri = 'https://api.calendly.com/event_types/TYPE0001';
const bookingUrl = 'https://calendly.com/d/abcd-1234/30-minute-meeting';
const credentials: Credentials = { authMethodId: 'personal-access-token', accessToken: new SecretString('token') };
const maxSends = CreateSchedulingLinkDefinition.maxSends!;

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'scheduling-link-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

function newClient(answer: () => { status: number; body: unknown }) {
  const requests: ProviderRequest[] = [];
  const client = new CalendlyClient({}, new StaticCredentialProvider([[connection, credentials]]), {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body } = answer();
      return { status, headers: {}, body: Buffer.from(JSON.stringify(body)) };
    },
  });
  return { client, requests };
}

async function createLink(client: CalendlyClient, context: AttemptContext): Promise<MutationResult<SchedulingLink> | RetryError> {
  const call = { id: callId, connection, operation: CreateSchedulingLinkDefinition.operation, context } as unknown as Call;
  try { return await runMutation(client.createSchedulingLink(), call, { eventTypeUri }); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

describe('Calendly createSchedulingLink bounded resend', () => {
  it.each([500, 409])('resends an unknown HTTP %i until the send cap, then selects uncertain', async status => {
    const { client, requests } = newClient(() => ({ status, body: { title: 'Unknown' } }));
    const context = new AttemptContext();
    for (let attempt = 1; attempt < maxSends; attempt++) {
      context.attempt = attempt;
      expect(await createLink(client, context)).toBeInstanceOf(RetryError);
      expect(context.marker).toMatchObject({ state: 'claimed', sendCount: attempt });
    }
    context.attempt = maxSends;
    expect(await createLink(client, context)).toMatchObject({ branch: 'uncertain' });
    context.attempt++;
    expect(await createLink(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(maxSends);
  });

  it('records canResend before selecting providerRejected for a definite first-send refusal', async () => {
    const { client, requests } = newClient(() => ({ status: 400, body: { title: 'Invalid Argument' } }));
    const context = new AttemptContext();
    expect(await createLink(client, context)).toMatchObject({ branch: 'providerRejected' });
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain for a refusal after a possible send', async () => {
    let index = 0;
    const { client, requests } = newClient(() => ({ status: index++ === 0 ? 502 : 400, body: {} }));
    const context = new AttemptContext();
    expect(await createLink(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await createLink(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(2);
  });

  it('selects created from a saved booking URL without another POST', async () => {
    const { client, requests } = newClient(() => ({ status: 500, body: {} }));
    const context = new AttemptContext();
    context.attempt = 2;
    context.marker = { callId, attempt: 1, state: 'claimed', providerId: bookingUrl, sendCount: 1 };
    expect(await createLink(client, context)).toMatchObject({ branch: 'created', value: { bookingUrl, eventTypeUri } });
    expect(requests).toHaveLength(0);
  });

  it('returns Retry without sending for an untrusted checkpoint', async () => {
    const { client, requests } = newClient(() => ({ status: 201, body: {} }));
    const context = new AttemptContext();
    context.attempt = 2;
    context.marker = { callId: 'foreign', attempt: 1, state: 'claimed' };
    expect(await createLink(client, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(0);
  });
});
