// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { HelpScoutClient } from '../src/client.js';
import { ReplyToConversationDefinition } from '../src/generated/definitions.js';
import type { Credentials, CredentialSource as HelpScoutCredentialSource, ReplyToConversationInput } from '../src/generated/models.js';

const callId = '3f9f4c0b-1d7f-5034-a855-40824c3de676';
const note: ReplyToConversationInput = { conversationId: 501, text: 'Triage note', isInternalNote: true };
const credentials: Credentials = { appId: 'app-id', appSecret: new SecretString('app-secret'), accessToken: new SecretString('inert-access-token') };
const conversation = { id: 501, number: 12, threads: 1, type: 'email', status: 'active', state: 'published', subject: 'Billing question', mailboxId: 123,
  primaryCustomer: { id: 238604, type: 'customer', email: 'jane@acme.example.com' }, createdAt: '2026-09-29T22:46:22Z' };

class AttemptContext {
  heartbeat: unknown;
  attempt = 1;
  readonly runId = 'run';
  readonly flowId = 'helpscout-reply-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly firstAttemptAt = new Date('2026-01-01T00:00:00Z');
  deadline: Date;
  readonly cancellationSignal = new AbortController().signal;
  constructor(deadlineMs = 30_000) { this.deadline = new Date(Date.now() + deadlineMs); }
  advance(): void { this.attempt++; this.deadline = new Date(Date.now() + 30_000); }
  hasLastHeartbeatValue(): boolean { return this.heartbeat !== undefined; }
  getLastHeartbeatValue(): unknown { return this.heartbeat; }
  async recordHeartbeat(value: unknown): Promise<void> { this.heartbeat = value; }
  /** Read this call's marker state from either SDK heartbeat format. */
  markerState(): unknown {
    const value = this.heartbeat as Record<string, unknown> | undefined;
    if (!value) return undefined;
    if ('state' in value) return value.state;
    return (value.singleDispatchMarkers as Record<string, { state: string }> | undefined)?.[callId]?.state;
  }
}

type Send = (request: ProviderRequest) => Promise<ProviderResponse>;
type CredentialSource = { resolve: () => Promise<Credentials> };

function errorResponse(status: number): ProviderResponse { return { status, headers: {}, body: Buffer.from('{"message":"error"}') }; }

function newClient(send: Send, source: CredentialSource = { resolve: async () => credentials }) {
  const requests: ProviderRequest[] = [];
  const client = new HelpScoutClient({}, source as unknown as HelpScoutCredentialSource, { send: async request => { requests.push(request); return send(request); } });
  return { client, requests };
}

async function run(client: HelpScoutClient, context: AttemptContext, input: ReplyToConversationInput = note): Promise<MutationResult<unknown> | 'retry'> {
  const call = { id: callId, connection: { provider: 'helpscout', name: 'helpscout-test' }, operation: ReplyToConversationDefinition.operation, context } as unknown as Call;
  try { return await runMutation(client.replyToConversation(), call, input); }
  catch (error) {
    if (error instanceof RetryError) return 'retry';
    throw error;
  }
}

function branchOf(result: MutationResult<unknown> | 'retry'): string { return result === 'retry' ? result : result.branch; }
function posts(requests: ProviderRequest[]): number { return requests.filter(request => request.method === 'POST').length; }

describe('Help Scout fenced reply decisions', () => {
  it('selects uncertain for an accepted reply without a thread ID and never resends it', async () => {
    const { client, requests } = newClient(async () => ({ status: 201, headers: { 'resource-id': 'not-a-number' }, body: new Uint8Array() }));
    expect(branchOf(await run(client, new AttemptContext()))).toBe('uncertain');
    expect(posts(requests)).toBe(1);
  });

  it('keeps the thread ID when the body breaks after 2xx headers and before any body byte', async () => {
    const { client, requests } = newClient(async () => { throw new ProviderTransportError(false, 201, { 'resource-id': '568' }); });
    const result = await run(client, new AttemptContext());
    expect(result).toMatchObject({ branch: 'replied', value: { threadId: 568 } });
    expect(posts(requests)).toBe(1);
  });

  it('passes the write-window signal and keeps the thread ID of an accepted reply it cuts off', async () => {
    let signal: AbortSignal | undefined;
    const { client, requests } = newClient(request => new Promise((_, reject) => {
      signal = request.signal;
      request.signal?.addEventListener('abort', () => reject(new ProviderTransportError(false, 201, { 'resource-id': '568' })), { once: true });
    }));
    const result = await run(client, new AttemptContext(600));
    expect(signal?.aborted).toBe(true);
    expect(result).toMatchObject({ branch: 'replied', value: { threadId: 568 } });
    expect(posts(requests)).toBe(1);
  });

  it('selects uncertain for a refusal after a rate-limited resend', async () => {
    const statuses = [502, 429, 422];
    const { client, requests } = newClient(async () => errorResponse(statuses.shift()!));
    const context = new AttemptContext();
    expect(await run(client, context)).toBe('retry');
    context.advance();
    expect(await run(client, context)).toBe('retry');
    expect(context.markerState()).toBe('canResend');
    context.advance();
    expect(branchOf(await run(client, context))).toBe('uncertain');
    expect(posts(requests)).toBe(3);
  });

  it.each([
    ['transient', new Error('token endpoint unavailable'), 'retry'],
    ['terminal', new ReauthorizationRequiredError(), 'uncertain'],
  ])('records nothing for a %s credential failure after a possible send', async (_name, error, expected) => {
    const first = newClient(async () => errorResponse(502));
    const context = new AttemptContext();
    expect(await run(first.client, context)).toBe('retry');
    context.advance();
    const failing = newClient(async () => errorResponse(502), { resolve: async () => { throw error; } });
    expect(branchOf(await run(failing.client, context))).toBe(expected);
    expect(context.markerState()).toBe('claimed');
    expect(posts(first.requests) + posts(failing.requests)).toBe(1);
  });

  it('records nothing for a primary customer read retry after a possible send', async () => {
    const reads = [{ status: 200, headers: {}, body: Buffer.from(JSON.stringify(conversation)) }, errorResponse(503)];
    const { client, requests } = newClient(async request => request.method === 'GET' ? reads.shift()! : errorResponse(502));
    const context = new AttemptContext();
    const reply: ReplyToConversationInput = { conversationId: 501, text: 'Response to Jane' };
    expect(await run(client, context, reply)).toBe('retry');
    context.advance();
    expect(await run(client, context, reply)).toBe('retry');
    expect(context.markerState()).toBe('claimed');
    expect(posts(requests)).toBe(1);
  });
});
