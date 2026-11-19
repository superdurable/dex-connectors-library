// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { MondayClient } from '../src/client.js';
import { decodeCredentials } from '../src/generated/models.js';

/** Return the error the SDK project provider throws when the generated decoder rejects stored material. */
function sdkWrappedMaterialError(): TypeError {
  try { decodeCredentials({ unexpected_field: 'value' }); }
  catch { return new TypeError('project credential decoding failed'); }
  throw new Error('malformed stored material decoded');
}
import { AddUpdateDefinition, CreateItemDefinition, UpdateItemColumnValuesDefinition } from '../src/generated/definitions.js';

const callId = 'd2f0e0a4-68c7-541e-aa07-d5ec3d088f1e';
const token = 'mondayTestToken0123456789abcdefSENTINELKEY';
const createdItem = { data: { create_item: { id: '9876543210', name: 'Fire drill', state: 'active', board: { id: '1234567890' }, group: { id: 'topics', title: 'February' } } } };
const addedUpdate = { data: { create_update: { id: '5550004', item_id: '9876543210' } } };

class AttemptContext {
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'monday-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  constructor(public attempt = 1, marker?: unknown) { this.marker = marker; }
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

function newClient(status: number, body: unknown = {}) {
  const requests: ProviderRequest[] = [];
  const credentials = async () => ({ authMethodId: 'personal-api-token', apiToken: new SecretString(token) });
  const client = new MondayClient({}, { resolve: credentials, resolveWithRefresh: credentials }, {
    apiURL: 'http://127.0.0.1/v2',
    send: async (request: ProviderRequest) => {
      requests.push(request);
      return { status, headers: {}, body: Buffer.from(typeof body === 'string' ? body : JSON.stringify(body)) };
    },
  });
  return { client, requests };
}

async function run(client: MondayClient, operation: 'createItem' | 'addUpdate' | 'updateItemColumnValues', context: AttemptContext, maxSendsOverride?: number): Promise<string> {
  const definition = operation === 'createItem' ? CreateItemDefinition : operation === 'addUpdate' ? AddUpdateDefinition : UpdateItemColumnValuesDefinition;
  const call = { id: callId, idempotencyKey: callId, connection: { provider: 'monday', name: 'monday-test' }, operation: definition.operation, context,
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try {
    let result: MutationResult<unknown>;
    if (operation === 'createItem') result = await runMutation(client.createItem(), call, { boardId: '1234567890', itemName: 'Fire drill' });
    else if (operation === 'addUpdate') result = await runMutation(client.addUpdate(), call, { itemId: '9876543210', body: 'Fire drill done' });
    else result = await runMutation(client.updateItemColumnValues(), call, { boardId: '1234567890', itemId: '9876543210', columnValues: { text: { type: 'text', text: 'Ready' } } });
    return result.branch;
  } catch (error) {
    if (error instanceof RetryError) return 'retry';
    throw error;
  }
}

describe('monday.com bounded resend of unconfirmed writes', () => {
  it('resends an unknown outcome below the cap under the same key, then selects uncertain', async () => {
    const { client, requests } = newClient(500);
    const context = new AttemptContext();
    for (; context.attempt < CreateItemDefinition.maxSends!; context.attempt++) {
      expect(await run(client, 'createItem', context)).toBe('retry');
      expect(context.marker).toMatchObject({ state: 'claimed', sendCount: context.attempt });
    }
    expect(await run(client, 'createItem', context)).toBe('uncertain');
    context.attempt++;
    expect(await run(client, 'createItem', context)).toBe('uncertain');
    expect(requests).toHaveLength(3);
    expect(new Set(requests.map(request => request.headers?.['Idempotency-Key']))).toEqual(new Set([callId]));
  });

  it('does not count a proven unprocessed refusal against the cap', async () => {
    const context = new AttemptContext();
    expect(await run(newClient(200, { errors: [{ extensions: { code: 'API_TEMPORARILY_BLOCKED' } }] }).client, 'addUpdate', context)).toBe('retry');
    expect(context.marker).toEqual({ callId, attempt: 1, state: 'canResend' });
    context.attempt = 2;
    expect(await run(newClient(200, addedUpdate).client, 'addUpdate', context)).toBe('added');
    expect(context.marker).toMatchObject({ state: 'claimed', providerId: '5550004', sendCount: 1 });
  });

  it('never resends with an application cap of one', async () => {
    const { client, requests } = newClient(409, { errors: [{ extensions: { code: 'IDEMPOTENCY_CONFLICT' } }] });
    expect(await run(client, 'addUpdate', new AttemptContext(), 1)).toBe('uncertain');
    expect(requests).toHaveLength(1);
  });

  it('never resends a write monday.com identified', async () => {
    const { client, requests } = newClient(200, createdItem);
    const context = new AttemptContext(2);
    expect(await run(client, 'createItem', context)).toBe('created');
    expect(context.marker).toMatchObject({ providerId: '9876543210' });
    context.attempt = 3;
    expect(await run(client, 'createItem', context)).toBe('uncertain');
    const unusable = newClient(200, { data: { create_item: { id: '9876543210', state: 'unknown' } } });
    const identified = new AttemptContext();
    expect(await run(unusable.client, 'createItem', identified)).toBe('uncertain');
    expect(identified.marker).toMatchObject({ providerId: '9876543210' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain for a refusal beside a mutation value and never resends it', async () => {
    const beside = { data: { create_item: { id: 'x' } }, errors: [{ extensions: { code: 'ColumnValueException' } }] };
    const { client, requests } = newClient(200, beside);
    expect(await run(client, 'createItem', new AttemptContext())).toBe('uncertain');
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain when a 2xx body breaks after its headers and never resends it', async () => {
    for (const failure of [new ProviderTransportError(false, 200, {}), new ProviderStalledError(200, {})]) {
      const requests: ProviderRequest[] = [];
      const credentials = async () => ({ authMethodId: 'personal-api-token', apiToken: new SecretString(token) });
      const client = new MondayClient({}, { resolve: credentials, resolveWithRefresh: credentials }, {
        apiURL: 'http://127.0.0.1/v2', send: async (request: ProviderRequest) => { requests.push(request); throw failure; },
      });
      const context = new AttemptContext();
      expect(await run(client, 'createItem', context)).toBe('uncertain');
      expect(context.marker).toMatchObject({ state: 'claimed' });
      expect(requests).toHaveLength(1);
    }
  });

  it('keeps counting toward the cap after a rate-limited resend', async () => {
    const context = new AttemptContext();
    for (const [attempt, status] of [[1, 500], [2, 429], [3, 500]] as const) {
      context.attempt = attempt;
      expect(await run(newClient(status).client, 'createItem', context)).toBe('retry');
    }
    context.attempt = 4;
    expect(await run(newClient(500).client, 'createItem', context)).toBe('uncertain');
  });

  it('never records notSent for a credential failure after a rate-limited resend', async () => {
    const context = new AttemptContext();
    for (const [attempt, status] of [[1, 500], [2, 429]] as const) {
      context.attempt = attempt;
      expect(await run(newClient(status).client, 'addUpdate', context)).toBe('retry');
    }
    const before = context.marker;
    const missing = async () => { throw new ObjectNotFoundError(); };
    const requests: ProviderRequest[] = [];
    const broken = new MondayClient({}, { resolve: missing, resolveWithRefresh: missing }, {
      apiURL: 'http://127.0.0.1/v2', send: async (request: ProviderRequest) => { requests.push(request); return { status: 200, headers: {}, body: Buffer.from(JSON.stringify(addedUpdate)) }; },
    });
    context.attempt = 3;
    expect(await run(broken, 'addUpdate', context)).toBe('uncertain');
    expect(context.marker).toBe(before);
    expect(requests).toHaveLength(0);
  });

  it.each([
    ['broker unavailable', () => new CredentialBrokerUnavailableError(), true],
    ['store read', () => new Error('project storage read timed out'), true],
    ['connection not configured', () => new Error('connection is not configured'), true],
    ['object not found', () => new ObjectNotFoundError(), false],
    ['connection not selected', () => new ConnectionNotSelectedError(), false],
    ['broker rejected', () => new CredentialBrokerRejectedError(), false],
    ['malformed stored material', () => sdkWrappedMaterialError(), false],
  ] as const)('retries a transient credential failure and selects defect only for a terminal one: %s', async (_name, newError, isTransient) => {
    const failing = async () => { throw newError(); };
    const requests: ProviderRequest[] = [];
    const broken = new MondayClient({}, { resolve: failing, resolveWithRefresh: failing }, {
      apiURL: 'http://127.0.0.1/v2', send: async (request: ProviderRequest) => { requests.push(request); return { status: 200, headers: {}, body: Buffer.from(JSON.stringify(addedUpdate)) }; },
    });
    const firstSend = new AttemptContext();
    expect(await run(broken, 'addUpdate', firstSend)).toBe(isTransient ? 'retry' : 'defect');
    // The checkpoint format differs across SDK releases, so match only the recorded state.
    if (isTransient) expect(JSON.stringify(firstSend.marker)).toContain('"state":"notSent"');
    const afterSend = new AttemptContext();
    expect(await run(newClient(500).client, 'addUpdate', afterSend)).toBe('retry');
    const before = afterSend.marker;
    afterSend.attempt = 2;
    expect(await run(broken, 'addUpdate', afterSend)).toBe(isTransient ? 'retry' : 'uncertain');
    expect(afterSend.marker).toBe(before);
    expect(requests).toHaveLength(0);
  });

  it('treats an oversized or broken refusal body as unknown and resends it below the cap', async () => {
    for (const failure of [new BodyTooLargeError(403, {}), new ProviderTransportError(false, 403, {})]) {
      const requests: ProviderRequest[] = [];
      const credentials = async () => ({ authMethodId: 'personal-api-token', apiToken: new SecretString(token) });
      const client = new MondayClient({}, { resolve: credentials, resolveWithRefresh: credentials }, {
        apiURL: 'http://127.0.0.1/v2', send: async (request: ProviderRequest) => { requests.push(request); throw failure; },
      });
      const context = new AttemptContext();
      for (; context.attempt < AddUpdateDefinition.maxSends!; context.attempt++) expect(await run(client, 'addUpdate', context)).toBe('retry');
      expect(await run(client, 'addUpdate', context)).toBe('uncertain');
      expect(requests).toHaveLength(AddUpdateDefinition.maxSends!);
      expect(requests[0]).not.toHaveProperty('ignoreErrorBodyReadFailure');
    }
  });

  it('selects uncertain when the write window aborts a 2xx body after its headers', async () => {
    const requests: ProviderRequest[] = [];
    const credentials = async () => ({ authMethodId: 'personal-api-token', apiToken: new SecretString(token) });
    const client = new MondayClient({}, { resolve: credentials, resolveWithRefresh: credentials }, {
      apiURL: 'http://127.0.0.1/v2',
      send: (request: ProviderRequest) => new Promise((_resolve, reject) => {
        requests.push(request);
        // The SDK keeps the status when an abort cuts a body whose headers arrived.
        request.signal!.addEventListener('abort', () => reject(new ProviderTransportError(false, 200, {})), { once: true });
      }),
    });
    const context = new AttemptContext();
    (context as { deadline: Date }).deadline = new Date(Date.now() + 7_000);
    expect(await run(client, 'createItem', context)).toBe('uncertain');
    expect(requests).toHaveLength(1);
  });

  it('records canResend for a definite refusal of a first send and never resends it', async () => {
    const { client, requests } = newClient(200, { errors: [{ extensions: { code: 'ColumnValueException' } }] });
    const context = new AttemptContext();
    expect(await run(client, 'createItem', context)).toBe('providerRejected');
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain for a refusal after a possible earlier send', async () => {
    const context = new AttemptContext();
    expect(await run(newClient(502).client, 'addUpdate', context)).toBe('retry');
    context.attempt = 2;
    expect(await run(newClient(403).client, 'addUpdate', context)).toBe('uncertain');
  });

  it('retries an untrusted checkpoint without sending', async () => {
    const { client, requests } = newClient(200, createdItem);
    const foreign = { callId: '8a2b6f7e-61b9-4d1a-9c55-2f3c1e0b9d41', attempt: 1, state: 'claimed' };
    expect(await run(client, 'createItem', new AttemptContext(2, foreign))).toBe('retry');
    expect(requests).toHaveLength(0);
  });

  it('retries every unknown absolute update and discards a response echoing the credential', async () => {
    expect(await run(newClient(418).client, 'updateItemColumnValues', new AttemptContext())).toBe('retry');
    expect(await run(newClient(302, '').client, 'updateItemColumnValues', new AttemptContext())).toBe('retry');
    expect(await run(newClient(200, { data: { change_multiple_column_values: { id: 'x' } } }).client, 'updateItemColumnValues', new AttemptContext())).toBe('retry');
    const reflected = { data: { change_multiple_column_values: { id: '9876543210', name: token } } };
    expect(await run(newClient(200, reflected).client, 'updateItemColumnValues', new AttemptContext())).toBe('invalidResponse');
    expect(await run(newClient(404).client, 'updateItemColumnValues', new AttemptContext())).toBe('notFound');
  });
});
