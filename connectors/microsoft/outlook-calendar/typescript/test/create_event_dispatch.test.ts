// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { Client } from '../src/client.js';
import type { CreateEventInput, Credentials } from '../src/generated/models.js';
import { HeartbeatContext, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const connection = { provider: 'microsoft', name: 'outlook-calendar-scheduler' };
const callId = '6d1d7c2e-9b44-5a4e-8f0b-3c2f4b1e7a10';
const credentials: Credentials = { authMethodId: 'microsoft-oauth', clientId: 'client', clientSecret: new SecretString('secret'),
  accessToken: new SecretString('rejected-token'), refreshToken: new SecretString('refresh') };
const createdEvent = { id: 'evt-1', subject: 'Kickoff', showAs: 'busy', type: 'singleInstance',
  start: { dateTime: '2026-02-24T17:00:00', timeZone: 'UTC' }, end: { dateTime: '2026-02-24T18:00:00', timeZone: 'UTC' },
  originalStartTimeZone: 'UTC', originalEndTimeZone: 'UTC' };
const input: CreateEventInput = { subject: 'Kickoff',
  start: { dateTime: '2026-02-24T09:00:00-08:00', timeZone: 'America/Los_Angeles' },
  end: { dateTime: '2026-02-24T10:00:00-08:00', timeZone: 'America/Los_Angeles' } };

function json(status: number, body: unknown): ProviderResponse {
  return { status, headers: {}, body: Buffer.from(JSON.stringify(body)) };
}

function lookupAnswer(isFound: boolean): ProviderResponse {
  return json(200, { value: isFound ? [createdEvent] : [] });
}

/** Throw after the headers arrive, as a connection lost mid-body does. */
function brokenAcceptedBody(request: ProviderRequest): never {
  request.onResponseChunk?.(new Uint8Array(), 201, {});
  throw new Error('connection reset mid-body');
}

async function runAttempts(client: Client, maxAttempts: number) {
  const context = new HeartbeatContext(undefined);
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    context.attempt = attempt;
    const call = scenarioCall({ callId, operation: 'createEvent' } as Scenario, connection, 'outlook-calendar', context,
      { idempotencyKey: callId }) as Call;
    const result = await client.createEvent().invoke(call, input);
    if (result.kind !== 'retry') return { result, attempts: attempt, context };
  }
  return { result: undefined, attempts: maxAttempts, context };
}

function refreshingSource(refreshErrors: unknown[]) {
  let forcedRefreshes = 0;
  return { resolve: async () => credentials, resolveWithRefresh: async () => credentials,
    resolveAfterRejection: async () => {
      forcedRefreshes++;
      if (forcedRefreshes <= refreshErrors.length) throw refreshErrors[forcedRefreshes - 1];
      return { ...credentials, accessToken: new SecretString('replacement-token') };
    } };
}

it('reads back a 2xx whose body breaks after the headers and never resends it', async () => {
  let posts = 0;
  const client = new Client({}, new StaticCredentialProvider([[connection, credentials]]), {
    baseURL: 'http://127.0.0.1:12345/v1.0',
    transport: async request => {
      if (request.method === 'GET') return lookupAnswer(posts > 0);
      posts++;
      return brokenAcceptedBody(request);
    },
  });
  const { result, attempts } = await runAttempts(client, 4);
  expect(result).toMatchObject({ kind: 'branch', branch: 'created', value: { event: { id: 'evt-1' } } });
  expect(attempts).toBe(1);
  expect(posts).toBe(1);
});

it('selects uncertain without a resend when a broken 2xx body is followed by a lagging lookup', async () => {
  let posts = 0;
  const client = new Client({}, new StaticCredentialProvider([[connection, credentials]]), {
    baseURL: 'http://127.0.0.1:12345/v1.0',
    transport: async request => {
      if (request.method === 'GET') return lookupAnswer(false);
      posts++;
      return brokenAcceptedBody(request);
    },
  });
  const { result, attempts } = await runAttempts(client, 4);
  expect(result).toMatchObject({ kind: 'uncertain' });
  expect(attempts).toBe(2);
  expect(posts).toBe(1);
});

it('retries a refused POST whose credential refresh fails transiently without counting it against the cap', async () => {
  let posts = 0;
  const unavailable = new Error('credential broker is unavailable');
  const client = new Client({}, refreshingSource([unavailable, unavailable, unavailable]), {
    baseURL: 'http://127.0.0.1:12345/v1.0',
    transport: async request => {
      if (request.method === 'GET') return lookupAnswer(false);
      posts++;
      const authorization = String((request.headers as Record<string, unknown>).Authorization);
      if (authorization === 'Bearer rejected-token') return json(401, { error: { code: 'InvalidAuthenticationToken' } });
      return json(201, createdEvent);
    },
  });
  const { result, attempts } = await runAttempts(client, 6);
  expect(result).toMatchObject({ kind: 'branch', branch: 'created' });
  expect(attempts).toBe(4);
  expect(posts).toBe(5);
});

it('rejects a refused POST whose credential refresh is terminal', async () => {
  let posts = 0;
  const client = new Client({}, refreshingSource([new ReauthorizationRequiredError()]), {
    baseURL: 'http://127.0.0.1:12345/v1.0',
    transport: async request => {
      if (request.method === 'GET') return lookupAnswer(false);
      posts++;
      return json(401, { error: { code: 'InvalidAuthenticationToken' } });
    },
  });
  const { result, attempts } = await runAttempts(client, 3);
  expect(result).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
  expect(attempts).toBe(1);
  expect(posts).toBe(1);
});
