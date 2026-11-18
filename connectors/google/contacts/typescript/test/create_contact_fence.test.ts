// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ContactsClient } from '../src/client.js';
import { CreateContactDefinition } from '../src/generated/definitions.js';
import type { Contact } from '../src/generated/models.js';

const callId = '74390522-858d-5ed6-bdf7-04e979ba9e4f';
const token = 'synthetic-contacts-token';

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'create-contact-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
  advance(): void { this.attempt++; this.deadline = new Date(Date.now() + 30_000); }
}

type Answer = (request: ProviderRequest) => ProviderResponse;

const respond = (status: number, body = ''): ProviderResponse => ({ status, headers: {}, body: Buffer.from(body) });

async function runAttempts(answer: Answer, attempts: number, failedResolution = 0): Promise<{ outcome: MutationResult<Contact> | RetryError; posts: number }> {
  let posts = 0;
  let resolutions = 0;
  const resolve = async () => {
    resolutions++;
    if (resolutions === failedResolution) throw new Error('credential refresh unavailable');
    return { accessToken: new SecretString(token), oauthClientId: 'client-id', oauthClientSecret: new SecretString('secret'), refreshToken: new SecretString('refresh') };
  };
  const client = new ContactsClient({ baseUrl: 'http://127.0.0.1:12345' }, { resolve }, {
    send: async request => {
      if (request.method === 'POST') posts++;
      return answer(request);
    },
  });
  const context = new AttemptContext();
  let outcome: MutationResult<Contact> | RetryError | undefined;
  for (let attempt = 1; attempt <= attempts; attempt++) {
    if (attempt > 1) {
      expect(outcome, `attempt ${attempt - 1}`).toBeInstanceOf(RetryError);
      context.advance();
    }
    const call = { id: callId, connection: { provider: 'google-contacts', name: 'google-contacts' }, operation: CreateContactDefinition.operation, context } as unknown as Call;
    try { outcome = await runMutation(client.createContact(), call, { givenName: 'Ada' }); }
    catch (error) {
      if (!(error instanceof RetryError)) throw error;
      outcome = error;
    }
  }
  return { outcome: outcome!, posts };
}

describe('Google Contacts createContact never resends an accepted create', () => {
  it.each([
    { name: 'unparsable body', answer: (() => respond(200, '{"resourceName":')) as Answer },
    { name: 'missing resource name', answer: (() => respond(200, '{"names":[]}')) as Answer },
    { name: 'accepted without body', answer: (() => respond(202)) as Answer },
    { name: 'success body that breaks', answer: (() => { throw new ProviderTransportError(false, 200, {}); }) as Answer },
  ])('selects uncertain for $name', async ({ answer }) => {
    const { outcome, posts } = await runAttempts(answer, 1);
    expect(outcome).toMatchObject({ branch: 'uncertain' });
    expect(posts).toBe(1);
  });

  it('reads back a resource name returned with an unknown status', async () => {
    const { outcome, posts } = await runAttempts(request => request.method === 'GET'
      ? respond(200, '{"resourceName":"people/c125","names":[{"displayName":"Ada"}]}')
      : respond(409, '{"resourceName":"people/c125"}'), 2);
    expect(outcome).toMatchObject({ branch: 'created', value: { resourceName: 'people/c125' } });
    expect(posts).toBe(1);
  });

  it('retries a read-back whose body breaks and never resends', async () => {
    let reads = 0;
    const { outcome, posts } = await runAttempts(request => {
      if (request.method === 'POST') return respond(409, '{"resourceName":"people/c126"}');
      reads++;
      if (reads === 1) throw new ProviderTransportError(false, 200, {});
      return respond(200, '{"resourceName":"people/c126","names":[{"displayName":"Ada"}]}');
    }, 3);
    expect(outcome).toMatchObject({ branch: 'created' });
    expect(posts).toBe(1);
    expect(reads).toBe(2);
  });

  it('treats an unreadable refusal as an unknown outcome', async () => {
    const { outcome, posts } = await runAttempts(() => respond(400, `{"error":"${token}"}`), 1);
    expect(outcome).toBeInstanceOf(RetryError);
    expect(posts).toBe(1);
  });

  it('keeps the send count across a pre-send retry after a possible send', async () => {
    const statuses = [500, 429, 500];
    // Attempt 1 is unknown, attempt 2 gets 429, attempt 3 fails credentials, attempt 4 is at the cap.
    const { outcome, posts } = await runAttempts(() => respond(statuses.shift() ?? 500), 4, 3);
    expect(outcome).toMatchObject({ branch: 'uncertain' });
    expect(posts).toBe(3);
  });
});
