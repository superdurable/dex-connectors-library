// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { HelpScoutClient } from '../src/client.js';
import { ReplyToConversationDefinition, UpdateConversationDefinition } from '../src/generated/definitions.js';
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

  it.each([412, 423])('treats a locked conversation status %i as an unknown outcome', async status => {
    const { client, requests } = newClient(async () => errorResponse(status));
    const context = new AttemptContext();
    expect(await run(client, context)).toBe('retry');
    expect(context.markerState()).toBe('claimed');
    expect(posts(requests)).toBe(1);
  });

  it.each([
    ['transient', new Error('token endpoint unavailable'), 'retry'],
    ['broker outage', new CredentialBrokerUnavailableError(), 'retry'],
    ['unconfigured static connection', new Error('connection is not configured'), 'retry'],
    ['terminal', new ReauthorizationRequiredError(), 'uncertain'],
    ['missing connection object', new ObjectNotFoundError(), 'uncertain'],
    ['unselected connection', new ConnectionNotSelectedError(), 'uncertain'],
    ['broker refusal', new CredentialBrokerRejectedError(), 'uncertain'],
    // The SDK project provider replaces a malformed-material decoder error with this TypeError.
    ['malformed stored material', new TypeError('project credential decoding failed'), 'uncertain'],
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

  it.each(incompleteRefusals)('treats a refusal whose %s as an unknown outcome', async (_name, refusal) => {
    let postCount = 0;
    const { client, requests } = newClient(async request => ++postCount === 1 ? refusal(request) : { status: 201, headers: { 'resource-id': '569' }, body: new Uint8Array() });
    const context = new AttemptContext();
    expect(await run(client, context)).toBe('retry');
    expect(context.markerState()).toBe('claimed');
    context.advance();
    expect(await run(client, context)).toMatchObject({ branch: 'replied', value: { threadId: 569 } });
    expect(posts(requests)).toBe(2);
  });

  it('reads back a refusal that names a thread and never resends it', async () => {
    const page = { _embedded: { threads: [{ id: 570, type: 'note', createdAt: '2026-09-30T09:00:00Z' }] } };
    const { client, requests } = newClient(async request => request.method === 'POST'
      ? { status: 422, headers: { 'resource-id': '570' }, body: Buffer.from('{"message":"error"}') }
      : { status: 200, headers: {}, body: Buffer.from(JSON.stringify(page)) });
    const context = new AttemptContext();
    expect(await run(client, context)).toBe('retry');
    context.advance();
    expect(await run(client, context)).toMatchObject({ branch: 'replied', value: { threadId: 570 } });
    expect(posts(requests)).toBe(1);
  });

  it.each(incompleteRefusals)('retries an update whose refusal %s', async (_name, refusal) => {
    const { client } = newClient(async request => request.method === 'GET'
      ? { status: 200, headers: {}, body: Buffer.from(JSON.stringify(conversation)) }
      : refusal(request));
    const call = { id: callId, connection: { provider: 'helpscout', name: 'helpscout-test' }, operation: UpdateConversationDefinition.operation, context: new AttemptContext() } as unknown as Call;
    await expect(runMutation(client.updateConversation(), call, { conversationId: 501, status: 'closed' })).rejects.toBeInstanceOf(RetryError);
  });

  it.each([
    ['an unlisted 409', (): ProviderResponse => errorResponse(409)],
    ['an unlisted 410', (): ProviderResponse => errorResponse(410)],
    ['an oversized 404 body', (): ProviderResponse => ({ status: 404, headers: {}, body: Buffer.alloc(70 << 10, 'a') })],
    ['an unparseable 200 body', (): ProviderResponse => ({ status: 200, headers: {}, body: Buffer.from('{"id":"broken"') })],
    ['a credential-echoing 404', (): ProviderResponse => ({ status: 404, headers: {}, body: Buffer.from('{"message":"inert-access-token"}') })],
  ])('retries %s on the read before the update, without writing', async (_name, read) => {
    const { client, requests } = newClient(async request => request.method === 'GET' ? read() : { status: 204, headers: {}, body: new Uint8Array() });
    const call = { id: callId, connection: { provider: 'helpscout', name: 'helpscout-test' }, operation: UpdateConversationDefinition.operation, context: new AttemptContext() } as unknown as Call;
    await expect(runMutation(client.updateConversation(), call, { conversationId: 501, status: 'closed' })).rejects.toBeInstanceOf(RetryError);
    expect(requests.filter(request => request.method !== 'GET')).toHaveLength(0);
  });

  it.each(preWriteLookupRefusals)('retries a 404 on the read before the update whose body %s, without writing', async (_name, read) => {
    const { client, requests } = newClient(async request => request.method === 'GET' ? read(request) : { status: 204, headers: {}, body: new Uint8Array() });
    const call = { id: callId, connection: { provider: 'helpscout', name: 'helpscout-test' }, operation: UpdateConversationDefinition.operation, context: new AttemptContext() } as unknown as Call;
    await expect(runMutation(client.updateConversation(), call, { conversationId: 501, status: 'closed' })).rejects.toBeInstanceOf(RetryError);
    expect(requests.filter(request => request.method !== 'GET')).toHaveLength(0);
  });

  it('resends a credential-echoing refusal below the cap and selects uncertain at the cap', async () => {
    const { client, requests } = newClient(async () => ({ status: 422, headers: {}, body: Buffer.from('{"message":"inert-access-token"}') }));
    const context = new AttemptContext();
    for (let attempt = 1; attempt < 3; attempt++) {
      expect(await run(client, context)).toBe('retry');
      expect(context.markerState()).toBe('claimed');
      context.advance();
    }
    expect(branchOf(await run(client, context))).toBe('uncertain');
    expect(posts(requests)).toBe(3);
  });

  it('retries an update whose write refusal echoes the access token', async () => {
    const { client } = newClient(async request => request.method === 'GET'
      ? { status: 200, headers: {}, body: Buffer.from(JSON.stringify(conversation)) }
      : { status: 422, headers: {}, body: Buffer.from('{"message":"inert-access-token"}') });
    const call = { id: callId, connection: { provider: 'helpscout', name: 'helpscout-test' }, operation: UpdateConversationDefinition.operation, context: new AttemptContext() } as unknown as Call;
    await expect(runMutation(client.updateConversation(), call, { conversationId: 501, status: 'closed' })).rejects.toBeInstanceOf(RetryError);
  });

  it('keeps a readable listed refusal on the read before the update definite', async () => {
    const { client } = newClient(async () => errorResponse(403));
    const call = { id: callId, connection: { provider: 'helpscout', name: 'helpscout-test' }, operation: UpdateConversationDefinition.operation, context: new AttemptContext() } as unknown as Call;
    await expect(runMutation(client.updateConversation(), call, { conversationId: 501, status: 'closed' })).resolves.toMatchObject({ branch: 'providerRejected' });
  });
});

/** A 422 whose error body streams past MaxErrorBodyBytes, as the provider HTTP sender reports chunks. */
async function oversizedRefusal(request: ProviderRequest): Promise<ProviderResponse> {
  const chunk = Buffer.alloc(16 << 10, 'a');
  const chunks: Uint8Array[] = [];
  for (let index = 0; index < 8; index++) {
    if (request.onResponseChunk?.(chunk, 422, {}) === false) break;
    chunks.push(chunk);
  }
  return { status: 422, headers: {}, body: Buffer.concat(chunks) };
}

/** A 404 whose body streams past MaxErrorBodyBytes, truncated as the sender does when the request caps error bodies. */
async function oversizedNotFound(request: ProviderRequest): Promise<ProviderResponse> {
  const chunk = Buffer.alloc(16 << 10, 'a');
  const chunks: Uint8Array[] = [];
  let length = 0;
  for (let index = 0; index < 8; index++) {
    if (request.onResponseChunk?.(chunk, 404, {}) === false) break;
    chunks.push(chunk); length += chunk.length;
    if (request.maxErrorResponseBytes !== undefined && length >= request.maxErrorResponseBytes) break;
  }
  const body = Buffer.concat(chunks);
  return { status: 404, headers: {}, body: request.maxErrorResponseBytes === undefined ? body : body.subarray(0, request.maxErrorResponseBytes) };
}

/** A 404 whose body breaks after the headers; the sender hides the break only when asked to. */
async function brokenNotFound(request: ProviderRequest): Promise<ProviderResponse> {
  if (request.ignoreErrorBodyReadFailure) return { status: 404, headers: {}, body: new Uint8Array() };
  throw new ProviderTransportError(false, 404, {});
}

const preWriteLookupRefusals = [
  ['streams past the error limit', oversizedNotFound],
  ['breaks after the headers', brokenNotFound],
] as const;

const incompleteRefusals = [
  ['body streams past the error limit', oversizedRefusal],
  ['body exceeds the response limit', async () => { throw new BodyTooLargeError(422, {}); }],
  ['body breaks after the headers', async () => { throw new ProviderTransportError(false, 400, {}); }],
] as const;
