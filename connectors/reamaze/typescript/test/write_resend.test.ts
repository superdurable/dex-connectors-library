// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { ReamazeClient } from '../src/client.js';
import { CreateConversationDefinition, ReplyToConversationDefinition, UpdateConversationDefinition } from '../src/generated/definitions.js';

const callId = '0d6f4bc4-7a51-5d24-9a38-6d8f4f6a1e10';
type WriteOperation = 'createConversation' | 'replyToConversation';

class AttemptContext {
  attempt = 1;
  heartbeat: unknown;
  heartbeatCount = 0;
  readonly runId = 'run';
  readonly flowId = 'reamaze-write-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline: Date;
  readonly cancellationSignal = new AbortController().signal;
  constructor(deadlineMs = 120_000) { this.deadline = new Date(Date.now() + deadlineMs); }
  hasLastHeartbeatValue(): boolean { return this.heartbeat !== undefined; }
  getLastHeartbeatValue(): unknown { return this.heartbeat; }
  async recordHeartbeat(value: unknown): Promise<void> { this.heartbeatCount++; this.heartbeat = value; }
  nextAttempt(): void { this.attempt++; this.heartbeatCount = 0; }
  /** This call's marker from a single marker or the checkpoint map. */
  get marker(): Record<string, unknown> | undefined {
    const value = this.heartbeat as Record<string, unknown> | undefined;
    const markers = value?.singleDispatchMarkers as Record<string, Record<string, unknown>> | undefined;
    return markers ? markers[callId] : value;
  }
}

function conversationWire(key: string): Record<string, unknown> {
  return { slug: 'double-charge', subject: 'Double charge', status: 0, created_at: '2026-01-26T14:02:00Z', tag_list: ['billing'], category: { slug: 'support', name: 'Support', channel: 1 }, data: { dex_dispatch_key: key } };
}
function messageWire(originId: string): Record<string, unknown> {
  return { body: 'Note.', visibility: 1, origin: 7, origin_id: originId, created_at: '2026-01-27T12:00:00Z' };
}
function page(name: string, entries: unknown[]): Record<string, unknown> {
  return { page_size: 30, page_count: entries.length ? 1 : 0, total_count: entries.length, [name]: entries };
}
function json(status: number, body: unknown): ProviderResponse { return { status, headers: {}, body: Buffer.from(JSON.stringify(body)) }; }
function keyOf(operation: WriteOperation, request: ProviderRequest): string {
  const payload = JSON.parse(Buffer.from(request.body!).toString('utf8')) as { conversation?: { data: { dex_dispatch_key: string } }; message?: { origin_id: string } };
  return operation === 'createConversation' ? payload.conversation!.data.dex_dispatch_key : payload.message!.origin_id;
}
function found(operation: WriteOperation, key: string, isFound: boolean): ProviderResponse {
  return operation === 'createConversation' ? json(200, page('conversations', isFound ? [conversationWire(key)] : [])) : json(200, page('messages', isFound ? [messageWire(key)] : []));
}

function newClient(answer: (request: ProviderRequest) => Promise<ProviderResponse> | ProviderResponse, options: { credentialError?: () => Error | undefined } = {}) {
  const requests: ProviderRequest[] = [];
  const credentials = { resolve: async () => {
    const error = options.credentialError?.();
    if (error) throw error;
    return { email: 'agent@acme.example.com', apiToken: new SecretString('reamazeTestToken0123456789') };
  } };
  const client = new ReamazeClient({ brand: 'acme' }, credentials, {
    apiBaseURL: 'http://127.0.0.1:12345/api/v1',
    send: async request => { requests.push(request); return answer(request); },
  });
  return { client, requests, methods: () => requests.map(request => request.method) };
}

async function run(client: ReamazeClient, operation: WriteOperation, context: AttemptContext): Promise<MutationResult<unknown> | RetryError> {
  const definition = operation === 'createConversation' ? CreateConversationDefinition : ReplyToConversationDefinition;
  const call = { id: callId, connection: { provider: 'reamaze', name: 'reamaze-test' }, operation: definition.operation, context } as unknown as Call;
  try {
    if (operation === 'createConversation') return await runMutation(client.createConversation(), call, { subject: 'Double charge', message: 'I was charged twice.', channel: 'support', requester: { email: 'jane@acme.example.com' } });
    return await runMutation(client.replyToConversation(), call, { conversationId: 'double-charge', text: 'Note.', isInternalNote: true });
  } catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

describe.each<WriteOperation>(['createConversation', 'replyToConversation'])('Re:amaze %s fenced write', operation => {
  const confirmedBranch = operation === 'createConversation' ? 'created' : 'replied';

  it.each([
    ['credentials resolve', undefined, 'confirmed'],
    ['credentials need reconnecting', new ReauthorizationRequiredError(), 'uncertain'],
    ['credential store outage', new Error('credential store outage'), 'retry'],
  ] as const)('reads back after a rate-limited resend of a possible send: %s', async (_, credentialError, want) => {
    let key = ''; let posts = 0; let reads = 0;
    const credentialState: { error?: Error } = {};
    const { client, requests, methods } = newClient(request => {
      if (request.method === 'POST') { key = keyOf(operation, request); return json(posts++ === 0 ? 502 : 429, {}); }
      // The first send's write becomes visible only after the rate-limited resend.
      return found(operation, key, ++reads > 1);
    }, { credentialError: () => credentialState.error });
    const context = new AttemptContext();
    expect(await run(client, operation, context)).toBeInstanceOf(RetryError);
    context.nextAttempt();
    expect(await run(client, operation, context)).toBeInstanceOf(RetryError);
    expect(methods()).toEqual(['POST', 'GET', 'POST']);
    expect(context.marker).toMatchObject({ state: 'canResend' });

    context.nextAttempt();
    if (credentialError) credentialState.error = credentialError;
    const result = await run(client, operation, context);
    if (want === 'confirmed') {
      expect(result).toMatchObject({ branch: confirmedBranch, value: { wasAlreadyApplied: true } });
      expect(methods()).toEqual(['POST', 'GET', 'POST', 'GET']);
      return;
    }
    if (want === 'uncertain') expect(result).toMatchObject({ branch: 'uncertain' });
    else expect(result).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(3);
    expect(context.heartbeatCount).toBe(0);
  });

  it.each([
    ['transport error', (status: number) => new ProviderTransportError(false, status, {})],
    ['stall', (status: number) => new ProviderStalledError(status, {})],
  ] as const)('never resends a 2xx whose body breaks after its headers: %s', async (_, bodyError) => {
    for (const isFound of [true, false]) {
      let key = '';
      const { client, methods } = newClient(request => {
        if (request.method === 'POST') { key = keyOf(operation, request); throw bodyError(201); }
        return found(operation, key, isFound);
      });
      const result = await run(client, operation, new AttemptContext());
      expect(result).toMatchObject({ branch: isFound ? confirmedBranch : 'uncertain' });
      expect(methods()).toEqual(['POST', 'GET']);
    }
  });

  it.each([true, false])('never resends a 2xx whose body the write window cuts off (found: %s)', async isFound => {
    let key = '';
    const { client, methods } = newClient(async request => {
      if (request.method !== 'POST') return found(operation, key, isFound);
      key = keyOf(operation, request);
      // The headers arrived; the SDK sender reports the cut body with the status it saw.
      await new Promise(resolve => request.signal?.addEventListener('abort', resolve, { once: true }));
      throw new ProviderTransportError(false, 201, {});
    });
    const result = await run(client, operation, new AttemptContext(600));
    expect(result).toMatchObject({ branch: isFound ? confirmedBranch : 'uncertain' });
    expect(methods()).toEqual(['POST', 'GET']);
  });

  it('selects uncertain when the read-back of an accepted write outlasts the attempt deadline', async () => {
    const { client, methods } = newClient(async request => {
      if (request.method === 'POST') {
        await new Promise(resolve => request.signal?.addEventListener('abort', resolve, { once: true }));
        throw new ProviderTransportError(false, 201, {});
      }
      return new Promise<ProviderResponse>(() => {});
    });
    expect(await run(client, operation, new AttemptContext(1_600))).toMatchObject({ branch: 'uncertain' });
    expect(methods()).toEqual(['POST', 'GET']);
  });

  it('selects uncertain when the read-back of an accepted write without a saved ID is rate limited', async () => {
    const { client, methods } = newClient(request => request.method === 'POST' ? json(201, { unexpected: true }) : json(429, {}));
    expect(await run(client, operation, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(methods()).toEqual(['POST', 'GET']);
  });

  it('selects uncertain when the read-back after a possible send is refused', async () => {
    const { client, methods } = newClient(request => json(request.method === 'POST' ? 502 : 404, {}));
    const context = new AttemptContext();
    expect(await run(client, operation, context)).toBeInstanceOf(RetryError);
    context.nextAttempt();
    expect(await run(client, operation, context)).toMatchObject({ branch: 'uncertain' });
    expect(methods()).toEqual(['POST', 'GET']);
  });

  it('retries a refusal whose body is too large to read', async () => {
    const { client } = newClient(() => { throw new BodyTooLargeError(422, {}); });
    const context = new AttemptContext();
    expect(await run(client, operation, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed' });
  });
});

it.each([
  ['malformed conversation', () => json(200, { slug: 'double-charge' })],
  ['oversized conversation', () => { throw new BodyTooLargeError(200, {}); }],
])('retries updateConversation when the read before the change is unusable: %s', async (_, answer) => {
  const { client, requests } = newClient(answer);
  const call = { id: callId, connection: { provider: 'reamaze', name: 'reamaze-test' }, operation: UpdateConversationDefinition.operation, context: new AttemptContext() } as unknown as Call;
  // An earlier attempt's PUT may have applied, so the safe write repeats.
  await expect(runMutation(client.updateConversation(), call, { conversationId: 'double-charge', status: 2 })).rejects.toBeInstanceOf(RetryError);
  expect(requests).toHaveLength(1);
});

it.each([
  ['an oversized refusal', () => { throw new BodyTooLargeError(400, {}); }, ''],
  ['a redirect', () => json(301, {}), ''],
  ['a conflict', () => json(409, {}), ''],
  ['an outage', () => json(500, {}), ''],
  ['a credential-reflecting refusal', () => json(400, { errors: 'reamazeTestToken0123456789' }), ''],
  ['a definite 403', () => json(403, {}), 'providerRejected'],
  ['a definite 404', () => json(404, {}), 'notFound'],
] as const)('selects an updateConversation branch on the first read only for a definite refusal: %s', async (_, answer, branch) => {
  const { client, methods } = newClient(answer);
  const call = { id: callId, connection: { provider: 'reamaze', name: 'reamaze-test' }, operation: UpdateConversationDefinition.operation, context: new AttemptContext() } as unknown as Call;
  const update = runMutation(client.updateConversation(), call, { conversationId: 'double-charge', status: 2 });
  if (branch) await expect(update).resolves.toMatchObject({ branch });
  else await expect(update).rejects.toBeInstanceOf(RetryError);
  expect(methods()).toEqual(['GET']);
});

it.each([
  ['reauthorization', () => new ReauthorizationRequiredError(), true],
  ['missing connection object', () => new ObjectNotFoundError(), true],
  ['unselected connection', () => new ConnectionNotSelectedError(), true],
  ['broker refusal', () => new CredentialBrokerRejectedError(), true],
  ['broker outage', () => new CredentialBrokerUnavailableError(), false],
  ['unconfigured static connection', () => new Error('connection is not configured'), false],
] as const)('classifies a %s credential failure the same way for every write', async (_, credentialError, isTerminal) => {
  for (const operation of ['createConversation', 'replyToConversation'] as const) {
    const { client, requests } = newClient(() => json(500, {}), { credentialError });
    const result = await run(client, operation, new AttemptContext());
    if (isTerminal) expect(result).toMatchObject({ branch: 'defect' });
    else expect(result).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(0);
  }
});

it('selects defect for a terminal updateConversation credential failure only on the first attempt', async () => {
  const { client, requests } = newClient(() => json(500, {}), { credentialError: () => new ReauthorizationRequiredError() });
  const context = new AttemptContext();
  const call = { id: callId, connection: { provider: 'reamaze', name: 'reamaze-test' }, operation: UpdateConversationDefinition.operation, context } as unknown as Call;
  await expect(runMutation(client.updateConversation(), call, { conversationId: 'double-charge', status: 2 })).resolves.toMatchObject({ branch: 'defect' });
  context.nextAttempt();
  // An earlier attempt's PUT may have applied, so the safe write repeats.
  await expect(runMutation(client.updateConversation(), call, { conversationId: 'double-charge', status: 2 })).rejects.toBeInstanceOf(RetryError);
  expect(requests).toHaveLength(0);
});

it('reads an accepted undecodable create back by its saved slug', async () => {
  let key = ''; let reads = 0;
  const { client, requests, methods } = newClient(request => {
    if (request.method === 'POST') { key = keyOf('createConversation', request); return json(201, { slug: 'double-charge', status: 'open' }); }
    expect(new URL(request.url).pathname).toBe('/api/v1/conversations/double-charge');
    return ++reads === 1 ? json(429, {}) : json(200, conversationWire(key));
  });
  const context = new AttemptContext();
  expect(await run(client, 'createConversation', context)).toBeInstanceOf(RetryError);
  expect(context.marker).toMatchObject({ providerId: 'double-charge' });
  context.nextAttempt();
  expect(await run(client, 'createConversation', context)).toMatchObject({ branch: 'created', value: { conversation: { id: 'double-charge' } } });
  expect(methods()).toEqual(['POST', 'GET', 'GET']);
  expect(requests).toHaveLength(3);
});
