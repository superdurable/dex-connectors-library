// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
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

async function post(context: AttemptContext, answer: (request: ProviderRequest) => Promise<ProviderResponse>, requests: ProviderRequest[]) {
  const client = new OAuth2HTTPClient(config, new StaticCredentialProvider([[connection, credentials]]), {
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

  it('selects uncertain without sending when a later attempt finds no checkpoint', async () => {
    const requests: ProviderRequest[] = [];
    const context = new AttemptContext();
    context.attempt = 2;
    expect((await post(context, respond(201), requests)).kind).toBe('uncertain');
    expect(requests).toHaveLength(0);
  });
});
