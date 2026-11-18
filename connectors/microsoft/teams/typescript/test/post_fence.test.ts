// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type MutationAttempt } from '@superdurable/connectors-sdk';
import { TeamsClient } from '../src/client.js';
import type { Credentials, PostMessageOutput } from '../src/generated/models.js';

const connection = { provider: 'microsoft', name: 'teams' };
const teamId = 'fbe2bf47-16c8-47cf-b4a5-4b9b187c508b';
const channelId = '19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2';
const chatId = '19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2';
const encoder = new TextEncoder();
const json = (status: number, value: unknown): ProviderResponse => ({ status, headers: {}, body: encoder.encode(JSON.stringify(value)) });
const badGateway = () => json(502, { error: { code: 'BadGateway' } });
const throttled = () => json(429, { error: { code: 'TooManyRequests' } });
const emptyListing = () => json(200, { value: [] });

/** Keeps the last heartbeat across Execute attempts like Dex does. */
class AttemptContext {
  attempt = 1;
  heartbeatCount = 0;
  heartbeatError: Error | undefined;
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'teams-flow';
  readonly flowStartedAt = new Date();
  readonly firstAttemptAt = new Date();
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> {
    if (this.heartbeatError) throw this.heartbeatError;
    this.heartbeatCount++;
    this.marker = value;
  }
}

/** Resolves a static token, failing transiently on the listed attempts. */
class AttemptCredentials implements CredentialProvider<Credentials> {
  constructor(readonly failingAttempts: number[] = []) {}
  async resolve(call: Call): Promise<Credentials> {
    if (this.failingAttempts.includes((call.context as unknown as AttemptContext).attempt)) throw new Error('token endpoint unavailable');
    return { accessToken: new SecretString('access-token'), oauthClientId: 'client', oauthClientSecret: new SecretString('secret'), refreshToken: new SecretString('refresh') };
  }
}

type Answer = (request: ProviderRequest, index: number) => Promise<ProviderResponse> | ProviderResponse;

async function post(answer: Answer, context: AttemptContext, methods: string[], operation: 'channel' | 'chat' = 'channel',
  credentials = new AttemptCredentials()): Promise<MutationAttempt<PostMessageOutput>> {
  const client = new TeamsClient({}, credentials, {
    transport: { send: async request => { methods.push(request.method); return answer(request, methods.length - 1); } },
  });
  const operationId = operation === 'chat' ? 'postChatMessage' : 'postChannelMessage';
  const call = { id: '74390522-858d-5ed6-bdf7-04e979ba9e4f', connection, operation: { connectorId: 'microsoft-teams', operationId }, context } as unknown as Call;
  return operation === 'chat'
    ? client.postChatMessage().invoke(call, { chatId, content: 'Escalate' })
    : client.postChannelMessage().invoke(call, { teamId, channelId, content: 'Incident' });
}

describe('fenced Microsoft Teams posts', () => {
  it.each(['channel', 'chat'] as const)('selects uncertain for an accepted %s post without a message ID', async operation => {
    const methods: string[] = [];
    const result = await post(() => json(201, { id: 42 }), new AttemptContext(), methods, operation);
    expect(result.kind).toBe('uncertain');
    expect(methods).toEqual(['POST']);
  });

  it('never resends an accepted chat post whose ID checkpoint fails', async () => {
    const context = new AttemptContext();
    const methods: string[] = [];
    const result = await post(() => { context.heartbeatError = new Error('dex unavailable'); return json(201, { id: '1616990032050' }); }, context, methods, 'chat');
    expect(result.kind).toBe('uncertain');
    expect(methods).toEqual(['POST']);
  });

  it('reads an accepted channel post with an unusable body back by its ID only', async () => {
    const context = new AttemptContext();
    const methods: string[] = [];
    const answer: Answer = request => {
      if (request.method === 'POST') return json(201, { id: '1616990032050', createdDateTime: 42 });
      expect(request.url).toContain('/messages/1616990032050');
      return json(404, { error: { code: 'NotFound' } });
    };
    expect((await post(answer, context, methods)).kind).toBe('retry');
    context.attempt = 2;
    expect((await post(answer, context, methods)).kind).toBe('uncertain');
    expect(methods).toEqual(['POST', 'GET']);
  });

  it.each(['channel', 'chat'] as const)('never resends an accepted %s post whose body breaks after the headers', async operation => {
    const methods: string[] = [];
    const answer: Answer = request => {
      request.onResponseChunk?.(encoder.encode('{"id":"16169'), 201, {});
      throw new ProviderTransportError(false);
    };
    const result = await post(answer, new AttemptContext(), methods, operation);
    expect(result.kind).toBe('uncertain');
    expect(methods).toEqual(['POST']);
  });

  it('reads back before sending after a throttled resend', async () => {
    const context = new AttemptContext();
    const methods: string[] = [];
    const answer: Answer = (request, index) => request.method === 'GET' ? emptyListing() : index === 0 ? badGateway() : throttled();
    expect((await post(answer, context, methods)).kind).toBe('retry');
    context.attempt = 2;
    expect((await post(answer, context, methods)).kind).toBe('retry');
    expect(methods).toEqual(['POST', 'GET', 'POST']);
    context.attempt = 3;
    expect((await post(answer, context, methods)).kind).toBe('retry');
    expect(methods.slice(3)).toEqual(['GET', 'POST']);
  });

  it('records nothing for a credential failure after a possible send', async () => {
    const context = new AttemptContext();
    const methods: string[] = [];
    const credentials = new AttemptCredentials([3]);
    const answer: Answer = (request, index) => request.method === 'GET' ? emptyListing() : index === 0 ? badGateway() : throttled();
    for (const attempt of [1, 2]) {
      context.attempt = attempt;
      expect((await post(answer, context, methods, 'channel', credentials)).kind).toBe('retry');
    }
    const heartbeatCount = context.heartbeatCount;
    context.attempt = 3;
    expect((await post(answer, context, methods, 'channel', credentials)).kind).toBe('retry');
    expect(context.heartbeatCount).toBe(heartbeatCount);
    context.attempt = 4;
    const requestCount = methods.length;
    expect((await post(answer, context, methods, 'channel', credentials)).kind).toBe('retry');
    expect(methods[requestCount]).toBe('GET');
  });

  it('never resends a definite refusal', async () => {
    const methods: string[] = [];
    const result = await post(() => json(403, { error: { code: 'Forbidden' } }), new AttemptContext(), methods);
    expect(result).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
    expect(methods).toEqual(['POST']);
  });
});
