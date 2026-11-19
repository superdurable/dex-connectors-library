// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerUnavailableError, ExchangePendingError } from '@superdurable/connectors-sdk/projectconfig';
import { ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { OAuth2HTTPClient } from '../src/client.js';
import { PostNonIdempotentDefinition } from '../src/generated/definitions.js';

const connection = { provider: 'oauth2-http', name: 'tested-api' };
const credentials = { oauthClientId: 'client', oauthClientSecret: new SecretString('client-secret'), accessToken: new SecretString('first-access-token'), refreshToken: new SecretString('') };
const config = { authorizationEndpoint: 'http://127.0.0.1:45880/auth', tokenEndpoint: 'http://127.0.0.1:45880/token', scopes: ['profile'], apiBaseUrl: 'http://127.0.0.1:45880/v1' };
const callId = '659e7b4f-b114-59f6-98d1-17c1143ef228';

/** Keeps the last heartbeat across Execute attempts as Dex does. */
class AttemptContext {
  attempt = 1;
  marker: unknown;
  isUnreadable = false;
  readonly cancellationSignal = new AbortController().signal;
  readonly deadline = new Date(Date.now() + 30_000);
  hasLastHeartbeatValue(): boolean {
    if (this.isUnreadable) throw new Error('heartbeat unavailable');
    return this.marker !== undefined;
  }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

/** Has no attempt time left, and loses every checkpoint write. */
class ExpiredAttemptContext extends AttemptContext {
  override readonly deadline = new Date(Date.now() - 1_000);
  override async recordHeartbeat(): Promise<void> { throw new Error('heartbeat unavailable'); }
}

async function post(context: AttemptContext, answer: (request: ProviderRequest) => Promise<ProviderResponse>, requests: ProviderRequest[], connectionCredentials = credentials) {
  const client = new OAuth2HTTPClient(config, new StaticCredentialProvider([[connection, connectionCredentials]]), {
    send: async request => { requests.push(request); return answer(request); },
  });
  const call = { id: callId, idempotencyKey: callId, connection, context } as unknown as Call;
  return client.postNonIdempotent().invoke(call, { path: '/orders', body: '{}' });
}

const respond = (status: number) => async (): Promise<ProviderResponse> => ({ status, headers: {}, body: Buffer.from('answer') });
const markerState = (context: AttemptContext) => (context.marker as { state?: string } | undefined)?.state;

describe('postNonIdempotent single dispatch', () => {
  it('is an unsafe operation', () => {
    expect(PostNonIdempotentDefinition.repeatOnUnknown).toBe('unsafe');
  });

  it.each([302, 408, 409, 500, 502])('selects uncertain for HTTP %i and never sends again', async status => {
    const requests: ProviderRequest[] = [];
    const context = new AttemptContext();
    expect((await post(context, respond(status), requests)).kind).toBe('uncertain');
    expect(markerState(context)).toBe('claimed');
    context.attempt = 2;
    expect((await post(context, respond(201), requests)).kind).toBe('uncertain');
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain when the answer is lost after the POST was sent', async () => {
    const attempt = await post(new AttemptContext(), async () => { throw new ProviderTransportError(false); }, []);
    expect(attempt.kind).toBe('uncertain');
  });

  it('records canResend and selects rejected for a definite refusal of the only send', async () => {
    const context = new AttemptContext();
    const attempt = await post(context, respond(422), []);
    expect(attempt.kind === 'branch' && attempt.branch).toBe('rejected');
    expect(markerState(context)).toBe('canResend');
  });

  it.each([429, 503])('returns Retry for HTTP %i and sends on the next attempt', async status => {
    const requests: ProviderRequest[] = [];
    const context = new AttemptContext();
    expect((await post(context, respond(status), requests)).kind).toBe('retry');
    expect(markerState(context)).toBe('canResend');
    context.attempt = 2;
    const attempt = await post(context, respond(201), requests);
    expect(attempt.kind === 'branch' && attempt.branch).toBe('completed');
    expect(requests).toHaveLength(2);
  });

  it('returns Retry for a POST that provably never left the process', async () => {
    const context = new AttemptContext();
    expect((await post(context, async () => { throw new ProviderTransportError(true); }, [])).kind).toBe('retry');
    expect(markerState(context)).toBe('canResend');
  });

  it('returns Retry without sending when the checkpoint cannot be read', async () => {
    const requests: ProviderRequest[] = [];
    const context = new AttemptContext();
    context.isUnreadable = true;
    expect((await post(context, respond(201), requests)).kind).toBe('retry');
    expect(requests).toHaveLength(0);
  });

  it('returns Retry without sending when no time remains and the notSent checkpoint fails', async () => {
    const requests: ProviderRequest[] = [];
    const context = new ExpiredAttemptContext();
    const attempt = await post(context, respond(201), requests);
    expect(attempt).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    expect(requests).toHaveLength(0);
  });

  it('never reports a driver AggregateError after the POST left the process as not sent', async () => {
    const attempt = await post(new AttemptContext(), async () => { throw new AggregateError([new Error('reset')], 'driver failure'); }, []);
    expect(attempt.kind).toBe('uncertain');
  });

  it('never records notSent for a credential failure after a possible earlier send', async () => {
    const requests: ProviderRequest[] = [];
    const context = new AttemptContext();
    // A canResend marker that still counts one possibly effective send keeps a send allowed.
    const seeded = { callId, attempt: 2, state: 'canResend', sendCount: 1 };
    context.marker = seeded;
    context.attempt = 3;
    const attempt = await post(context, respond(201), requests, { ...credentials, accessToken: new SecretString('') });
    expect(attempt.kind).toBe('uncertain');
    expect(context.marker).toBe(seeded);
    expect(requests).toHaveLength(0);
  });

  it('selects uncertain without sending while the checkpoint counts an earlier possible POST', async () => {
    const requests: ProviderRequest[] = [];
    const context = new AttemptContext();
    context.marker = { callId, attempt: 2, state: 'canResend', sendCount: 1 };
    context.attempt = 3;
    expect((await post(context, respond(201), requests)).kind, 'the send cap of one is used').toBe('uncertain');
    expect(requests).toHaveLength(0);
  });

  it('returns Retry for an unavailable credential broker and records notSent', async () => {
    const context = new AttemptContext();
    const client = new OAuth2HTTPClient(config, { resolve: async () => { throw new CredentialBrokerUnavailableError(); }, resolveWithRefresh: async () => { throw new CredentialBrokerUnavailableError(); } },
      { send: async () => { throw new Error('sent'); } });
    const call = { id: callId, idempotencyKey: callId, connection, context } as unknown as Call;
    expect((await client.postNonIdempotent().invoke(call, { path: '/orders' })).kind).toBe('retry');
    expect(markerState(context)).toBe('notSent');
  });

  it('selects uncertain for a terminal credential failure of a later sendRequest write', async () => {
    const client = new OAuth2HTTPClient(config, { resolve: async () => { throw new ConnectionNotSelectedError(); }, resolveWithRefresh: async () => { throw new ConnectionNotSelectedError(); } },
      { send: async () => { throw new Error('sent'); } });
    for (const [method, attempt, branch] of [['POST', 1, 'defect'], ['POST', 2, 'uncertain'], ['DELETE', 2, 'uncertain'], ['GET', 2, 'defect']] as const) {
      const context = new AttemptContext();
      context.attempt = attempt;
      const call = { id: callId, idempotencyKey: callId, connection, context } as unknown as Call;
      const result = await client.sendRequest().invoke(call, { method, path: '/orders' });
      expect(result.kind === 'retry' ? 'retry' : result.kind === 'uncertain' ? 'uncertain' : result.branch, `${method} attempt ${attempt}`).toBe(branch);
    }
    const broker = new OAuth2HTTPClient(config, { resolve: async () => { throw new ExchangePendingError(); }, resolveWithRefresh: async () => { throw new ExchangePendingError(); } });
    const pending = { id: callId, idempotencyKey: callId, connection, context: new AttemptContext() } as unknown as Call;
    expect((await broker.sendRequest().invoke(pending, { method: 'POST', path: '/orders' })).kind).toBe('retry');
  });

  it('selects uncertain for a definite refusal of a later sendRequest write', async () => {
    const client = new OAuth2HTTPClient(config, new StaticCredentialProvider([[connection, credentials]]), { send: respond(422) });
    for (const [method, attempt, branch] of [['POST', 1, 'rejected'], ['POST', 2, 'uncertain'], ['PUT', 3, 'uncertain'], ['GET', 2, 'rejected']] as const) {
      const context = new AttemptContext();
      context.attempt = attempt;
      const call = { id: callId, idempotencyKey: callId, connection, context } as unknown as Call;
      const result = await client.sendRequest().invoke(call, { method, path: '/orders' });
      expect(result.kind === 'retry' ? 'retry' : result.kind === 'uncertain' ? 'uncertain' : result.branch, `${method} attempt ${attempt}`).toBe(branch);
      if (result.kind !== 'retry') expect(result.value.statusCode).toBe(422);
    }
  });

  it('selects uncertain without sending when a later attempt finds no checkpoint', async () => {
    const requests: ProviderRequest[] = [];
    const context = new AttemptContext();
    context.attempt = 2;
    expect((await post(context, respond(201), requests)).kind).toBe('uncertain');
    expect(requests).toHaveLength(0);
  });
});
