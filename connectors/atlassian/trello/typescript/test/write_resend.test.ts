// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, MaxErrorBodyBytes, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { TrelloClient } from '../src/client.js';
import { AddCommentDefinition, CreateCardDefinition, UpdateCardDefinition } from '../src/generated/definitions.js';
import { CredentialMaterialError } from '../src/generated/models.js';

const callId = 'fed5a116-c5f6-53ac-81b1-a823fb443a88';
const listId = '6512f0a1c2d3e4f5a6b70101';
const cardId = '6512f0a1c2d3e4f5a6b7c8d9';

class AttemptContext {
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'trello-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  constructor(public attempt = 1, marker?: unknown) { this.marker = marker; }
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

function newClient(status: number, body = '{}') {
  const requests: ProviderRequest[] = [];
  const client = new TrelloClient({}, { resolve: async () => ({ apiKey: new SecretString('test-key'), token: new SecretString('test-token') }) }, {
    send: async (request: ProviderRequest) => { requests.push(request); return { status, headers: {}, body: Buffer.from(body) }; },
  });
  return { client, requests };
}

async function run(client: TrelloClient, operation: 'createCard' | 'addComment', context: AttemptContext, maxSendsOverride?: number): Promise<string> {
  const definition = operation === 'createCard' ? CreateCardDefinition : AddCommentDefinition;
  const call = { id: callId, connection: { provider: 'trello', name: 'trello-test' }, operation: definition.operation, context, ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try {
    const result: MutationResult<unknown> = operation === 'createCard'
      ? await runMutation(client.createCard(), call, { listId, name: 'Approved request' })
      : await runMutation(client.addComment(), call, { cardId, text: 'Approved.' });
    return result.branch;
  } catch (error) {
    if (error instanceof RetryError) return 'retry';
    throw error;
  }
}

describe('Trello bounded resend of unconfirmed writes', () => {
  it('resends an unknown outcome below the cap, then selects uncertain', async () => {
    const { client, requests } = newClient(500);
    const context = new AttemptContext();
    for (; context.attempt < CreateCardDefinition.maxSends!; context.attempt++) {
      expect(await run(client, 'createCard', context)).toBe('retry');
      expect(context.marker).toMatchObject({ state: 'claimed', sendCount: context.attempt });
    }
    expect(await run(client, 'createCard', context)).toBe('uncertain');
    context.attempt++;
    expect(await run(client, 'createCard', context)).toBe('uncertain');
    expect(requests).toHaveLength(3);
  });

  it('never resends with a send cap of one', async () => {
    const { client, requests } = newClient(409);
    expect(await run(client, 'addComment', new AttemptContext(), 1)).toBe('uncertain');
    expect(requests).toHaveLength(1);
  });

  it('resends on a later attempt and never resends a saved identity', async () => {
    const { client, requests } = newClient(200, `{"id":"${cardId}"}`);
    const context = new AttemptContext(2);
    expect(await run(client, 'createCard', context)).toBe('created');
    expect(context.marker).toMatchObject({ providerId: cardId });
    context.attempt = 3;
    expect(await run(client, 'createCard', context)).toBe('uncertain');
    expect(await run(newClient(200, `{"id":"${cardId}"}`).client, 'addComment', new AttemptContext(AddCommentDefinition.maxSends! + 1))).toBe('uncertain');
    expect(requests).toHaveLength(1);
  });

  it('records canResend on a definite refusal and never resends it', async () => {
    const { client, requests } = newClient(400, 'invalid value for idList');
    const context = new AttemptContext();
    expect(await run(client, 'createCard', context)).toBe('providerRejected');
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain when a resend is refused', async () => {
    const context = new AttemptContext();
    expect(await run(newClient(502).client, 'addComment', context)).toBe('retry');
    context.attempt = 2;
    expect(await run(newClient(401, 'invalid token').client, 'addComment', context)).toBe('uncertain');
  });

  for (const operation of ['createCard', 'addComment'] as const) {
    it(`never resends an accepted ${operation} with an unusable body`, async () => {
      const { client, requests } = newClient(200, 'not json');
      expect(await run(client, operation, new AttemptContext())).toBe('uncertain');
      expect(requests).toHaveLength(1);
    });

    it(`never resends an accepted ${operation} whose body broke`, async () => {
      const requests: ProviderRequest[] = [];
      const client = new TrelloClient({}, { resolve: async () => ({ apiKey: new SecretString('test-key'), token: new SecretString('test-token') }) }, {
        send: async (request: ProviderRequest) => { requests.push(request); throw new ProviderTransportError(false, 200, {}); },
      });
      expect(await run(client, operation, new AttemptContext())).toBe('uncertain');
      expect(requests).toHaveLength(1);
    });
  }

  for (const [name, failure] of [
    ['an interrupted 400', () => new ProviderTransportError(false, 400, {})],
    ['an oversized 404', () => new BodyTooLargeError(404, {})],
  ] as const) {
    it(`treats ${name} as an unknown outcome`, async () => {
      const requests: ProviderRequest[] = [];
      const client = new TrelloClient({}, { resolve: async () => ({ apiKey: new SecretString('test-key'), token: new SecretString('test-token') }) }, {
        send: async (request: ProviderRequest) => { requests.push(request); throw failure(); },
      });
      const context = new AttemptContext();
      expect(await run(client, 'addComment', context)).toBe('retry');
      expect(context.marker).toMatchObject({ state: 'claimed', sendCount: 1 });
      expect(requests).toHaveLength(1);
    });
  }

  it('treats a 404 whose body exceeds the error-body limit as an unknown outcome', async () => {
    const { client } = newClient(404, 'x'.repeat(MaxErrorBodyBytes + 1));
    const context = new AttemptContext();
    expect(await run(client, 'createCard', context)).toBe('retry');
    expect(context.marker).toMatchObject({ state: 'claimed' });
  });

  it('retries a transient credential failure and selects uncertain for a terminal one after a possible send', async () => {
    const failingClient = (error: Error) => new TrelloClient({}, { resolve: async () => { throw error; } }, {
      send: async () => { throw new Error('no request is expected'); },
    });
    const first = new AttemptContext();
    expect(await run(failingClient(new Error('credential store unavailable')), 'createCard', first)).toBe('retry');
    expect(first.marker).toMatchObject({ state: 'canResend' });
    expect(await run(failingClient(new ReauthorizationRequiredError()), 'createCard', new AttemptContext())).toBe('defect');
    const earlier = { callId, attempt: 2, state: 'canResend', sendCount: 1 };
    const later = new AttemptContext(3, earlier);
    expect(await run(failingClient(new Error('credential store unavailable')), 'createCard', later)).toBe('retry');
    expect(later.marker).toMatchObject({ state: 'canResend', sendCount: 1 });
    expect(await run(failingClient(new ReauthorizationRequiredError()), 'createCard', new AttemptContext(3, earlier))).toBe('uncertain');
  });

  it.each([
    ['not selected', () => new ConnectionNotSelectedError(), 'defect'],
    ['broker rejected', () => new CredentialBrokerRejectedError(), 'defect'],
    ['object not found', () => new ObjectNotFoundError(), 'defect'],
    ['malformed stored material', () => new CredentialMaterialError('stored credential does not match the connector credential fields'), 'defect'],
    ['SDK decoding failure', () => new TypeError('project credential decoding failed'), 'defect'],
    ['broker unavailable', () => new CredentialBrokerUnavailableError(), 'retry'],
    ['not configured', () => new Error('connection is not configured'), 'retry'],
    ['unlisted type error', () => new TypeError('unlisted'), 'retry'],
  ])('classifies a credential resolve failure (%s) before any send', async (_name, newError, expected) => {
    const error = newError();
    const client = new TrelloClient({}, { resolve: async () => { throw error; } }, { send: async () => { throw new Error('no request is expected'); } });
    expect(await run(client, 'createCard', new AttemptContext())).toBe(expected);
  });

  it('selects defect for header-unsafe resolved credentials', async () => {
    const client = new TrelloClient({}, { resolve: async () => ({ apiKey: new SecretString('key'), token: new SecretString('to"ken') }) }, { send: async () => { throw new Error('no request is expected'); } });
    expect(await run(client, 'createCard', new AttemptContext())).toBe('defect');
  });

  it('retries an update whose refusal body broke', async () => {
    const requests: ProviderRequest[] = [];
    const client = new TrelloClient({}, { resolve: async () => ({ apiKey: new SecretString('test-key'), token: new SecretString('test-token') }) }, {
      send: async (request: ProviderRequest) => { requests.push(request); throw new ProviderTransportError(false, 400, {}); },
    });
    const call = { id: callId, connection: { provider: 'trello', name: 'trello-test' }, operation: UpdateCardDefinition.operation, context: new AttemptContext() } as unknown as Call;
    await expect(runMutation(client.updateCard(), call, { cardId, isClosed: true })).rejects.toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(1);
  });

  it('retries an untrusted checkpoint without sending', async () => {
    const { client, requests } = newClient(200, `{"id":"${cardId}"}`);
    expect(await run(client, 'createCard', new AttemptContext(2, { callId: 'another-call', attempt: 1, state: 'claimed' }))).toBe('retry');
    expect(requests).toEqual([]);
  });
});
