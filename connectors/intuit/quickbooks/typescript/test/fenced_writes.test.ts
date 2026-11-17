// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QuickBooksClient } from '../src/client.js';
import { CreateCustomerDefinition } from '../src/generated/definitions.js';
import type { CreateCustomerOutput } from '../src/generated/models.js';

const callId = '0fda9033-4ba6-5462-98fa-01d400ec4987';
const customer = { Customer: { Id: '58', SyncToken: '0', DisplayName: 'City Agency', PrimaryEmailAddr: { Address: 'accounts@cityagency.example.com' }, Active: true, Balance: 0 } };
const nameConflict = { Fault: { type: 'ValidationFault', Error: [{ code: '6240' }] } };
const credentials = { accessToken: new SecretString('intuit-test-access-token-0123456789'), clientId: 'ABtestQuickBooksClientID0123456789', clientSecret: new SecretString('client-secret'), refreshToken: new SecretString(''), idToken: new SecretString('') };

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'quickbooks-fenced-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

interface Reply { status: number; body: unknown; headers?: Record<string, string> }

/** Answers each request with the next scripted reply, repeating the last. */
class Script {
  readonly requests: ProviderRequest[] = [];
  constructor(private readonly replies: Reply[]) {}
  async send(request: ProviderRequest): Promise<ProviderResponse> {
    this.requests.push(request);
    const reply = this.replies[Math.min(this.requests.length, this.replies.length) - 1]!;
    return { status: reply.status, headers: reply.headers ?? {}, body: Buffer.from(JSON.stringify(reply.body)) };
  }
}

async function create(script: Script, context: AttemptContext, maxSendsOverride?: number): Promise<MutationResult<CreateCustomerOutput> | RetryError> {
  const client = new QuickBooksClient({ realmId: '9341453050298464' }, { resolve: async () => credentials, resolveWithRefresh: async () => credentials },
    { send: request => script.send(request), localProviderURL: 'http://127.0.0.1:12345' });
  const call = { id: callId, connection: { provider: 'quickbooks', name: 'quickbooks-test' }, operation: CreateCustomerDefinition.operation, context,
    ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try { return await runMutation(client.createCustomer(), call, { displayName: 'City Agency', emailAddress: 'accounts@cityagency.example.com' }); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

function requestID(request: ProviderRequest): string | null { return new URL(request.url).searchParams.get('requestid'); }

describe('QuickBooks bounded resend of writes', () => {
  it('resends an unknown outcome with the same requestid below the cap', async () => {
    for (const status of [500, 409, 501, 425]) {
      const script = new Script([{ status, body: {} }, { status: 200, body: customer }]);
      const context = new AttemptContext();
      expect(await create(script, context)).toBeInstanceOf(RetryError);
      expect(context.marker).toMatchObject({ state: 'claimed', sendCount: 1 });
      context.attempt = 2;
      expect(await create(script, context)).toMatchObject({ branch: 'created' });
      expect(requestID(script.requests[0]!)).toBe(requestID(script.requests[1]!));
    }
  });

  it('selects uncertain only at the send cap', async () => {
    const script = new Script([{ status: 500, body: {} }]);
    const context = new AttemptContext();
    for (let attempt = 1; attempt < CreateCustomerDefinition.maxSends!; attempt++) {
      context.attempt = attempt;
      expect(await create(script, context)).toBeInstanceOf(RetryError);
    }
    context.attempt = CreateCustomerDefinition.maxSends!;
    expect(await create(script, context)).toMatchObject({ branch: 'uncertain' });
    context.attempt++;
    expect(await create(script, context)).toMatchObject({ branch: 'uncertain' });
    expect(script.requests).toHaveLength(CreateCustomerDefinition.maxSends!);
    expect(await create(new Script([{ status: 500, body: {} }]), new AttemptContext(), 1)).toMatchObject({ branch: 'uncertain' });
  });

  it('reads back an accepted unusable write by its saved ID without resending', async () => {
    const script = new Script([{ status: 200, body: { Customer: { Id: '58' } } }, { status: 503, body: {} }, { status: 200, body: customer }]);
    const context = new AttemptContext();
    expect(await create(script, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ providerId: '58' });
    context.attempt = 2;
    expect(await create(script, context)).toMatchObject({ branch: 'created' });
    expect(script.requests.map(request => request.method)).toEqual(['POST', 'GET', 'GET']);
  });

  it('selects a definite refusal of a first write after recording canResend', async () => {
    const context = new AttemptContext();
    expect(await create(new Script([{ status: 400, body: nameConflict }]), context)).toMatchObject({ branch: 'nameConflict' });
    expect(context.marker).toMatchObject({ state: 'canResend' });
  });

  it('selects uncertain for a refusal after a possible earlier write', async () => {
    const script = new Script([{ status: 502, body: {} }, { status: 400, body: nameConflict }]);
    const context = new AttemptContext();
    expect(await create(script, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await create(script, context)).toMatchObject({ branch: 'uncertain' });
  });

  it('retries a 429 or 503 write without counting it', async () => {
    for (const status of [429, 503]) {
      const script = new Script([{ status, body: {}, headers: { 'retry-after': '1' } }, { status: 200, body: customer }]);
      const context = new AttemptContext();
      expect(await create(script, context)).toBeInstanceOf(RetryError);
      expect(context.marker).toMatchObject({ state: 'canResend' });
      expect((context.marker as { sendCount?: number }).sendCount).toBeUndefined();
      context.attempt = 2;
      expect(await create(script, context)).toMatchObject({ branch: 'created' });
      expect(context.marker).toMatchObject({ sendCount: 1 });
    }
  });

  it('retries an unreadable checkpoint without a request', async () => {
    const script = new Script([{ status: 200, body: customer }]);
    const context = new AttemptContext();
    context.marker = 'not a checkpoint';
    context.attempt = 2;
    expect(await create(script, context)).toBeInstanceOf(RetryError);
    expect(script.requests).toHaveLength(0);
  });
});
