// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation, type MutationResult } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { PipedriveClient } from '../src/client.js';
import { CreateObjectDefinition, UpdateObjectDefinition, UpsertObjectDefinition } from '../src/generated/definitions.js';

const callId = 'caa4781a-357f-5359-9562-1241cba08988';
const emptySearch = '{"success":true,"data":{"items":[]},"additional_data":{"next_cursor":null}}';
const person = '{"success":true,"data":{"id":901,"name":"Jane Smith","emails":[{"value":"jane@acme.example.com","primary":true}]}}';

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'pipedrive-write-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

interface PipedriveAnswer { status: number; body: string }

function newClient(answer: (request: ProviderRequest) => PipedriveAnswer) {
  const requests: ProviderRequest[] = [];
  const credentials = async () => ({ authMethodId: 'api-token', apiToken: new SecretString('inert-token') });
  const client = new PipedriveClient({ endpoint: 'http://127.0.0.1:42703' }, { resolve: credentials, resolveWithRefresh: credentials }, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body } = answer(request);
      return { status, headers: {}, body: Buffer.from(body) };
    },
  });
  return { client, requests };
}

async function run<Input, Output>(mutation: Mutation<Input, Output>, operation: unknown, input: Input, context: AttemptContext): Promise<MutationResult<Output> | RetryError> {
  const call = { id: callId, connection: { provider: 'pipedrive', name: 'pipedrive-crm' }, operation, context } as unknown as Call;
  try { return await runMutation(mutation, call, input); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

const deal = { objectType: 'deals', fields: { title: 'Acme renewal' } };

describe('createObject bounded resend', () => {
  it.each([500, 409, 402, 302, 418])('resends an unknown HTTP %i until the send cap, then selects uncertain', async status => {
    const { client, requests } = newClient(() => ({ status, body: '{"success":false}' }));
    const context = new AttemptContext();
    for (const attempt of [1, 2]) {
      context.attempt = attempt;
      expect(await run(client.createObject(), CreateObjectDefinition.operation, deal, context)).toBeInstanceOf(RetryError);
    }
    context.attempt = 3;
    expect(await run(client.createObject(), CreateObjectDefinition.operation, deal, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(3);
  });

  it('records canResend before selecting providerRejected for a definite first-send refusal', async () => {
    const { client } = newClient(() => ({ status: 400, body: '{"success":false}' }));
    const context = new AttemptContext();
    expect(await run(client.createObject(), CreateObjectDefinition.operation, deal, context)).toMatchObject({ branch: 'providerRejected' });
    expect(context.marker).toMatchObject({ state: 'canResend' });
  });

  it('selects uncertain for a refusal after a possible send', async () => {
    let index = 0;
    const { client, requests } = newClient(() => ({ status: index++ === 0 ? 502 : 400, body: '{"success":false}' }));
    const context = new AttemptContext();
    expect(await run(client.createObject(), CreateObjectDefinition.operation, deal, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await run(client.createObject(), CreateObjectDefinition.operation, deal, context)).toMatchObject({ branch: 'uncertain' });
    expect(requests).toHaveLength(2);
  });

  it('never resends an accepted create whose body cannot be read', async () => {
    const { ProviderTransportError } = await import('@superdurable/connectors-sdk/providerhttp');
    let sends = 0;
    const credentials = async () => ({ authMethodId: 'api-token', apiToken: new SecretString('inert-token') });
    const client = new PipedriveClient({ endpoint: 'http://127.0.0.1:42703' }, { resolve: credentials, resolveWithRefresh: credentials }, {
      send: async () => { sends++; throw new ProviderTransportError(false, 201, {}); },
    });
    expect(await run(client.createObject(), CreateObjectDefinition.operation, deal, new AttemptContext())).toMatchObject({ branch: 'uncertain' });
    expect(sends).toBe(1);
  });
});

describe('upsertObject bounded resend', () => {
  it.each([
    { priorAttempt: 1, branch: 'upserted', methods: ['GET', 'POST'] },
    { priorAttempt: 3, branch: 'uncertain', methods: ['GET'] },
  ])('searches after $priorAttempt possible sends and selects $branch', async ({ priorAttempt, branch, methods }) => {
    const { client, requests } = newClient(request => request.method === 'GET' ? { status: 200, body: emptySearch } : { status: 201, body: person });
    const context = new AttemptContext();
    context.marker = { callId, attempt: priorAttempt, state: 'claimed', sendCount: priorAttempt };
    context.attempt = priorAttempt + 1;
    const input = { objectType: 'persons', idProperty: 'email', idValue: 'jane@acme.example.com', fields: { name: 'Jane Smith' } };
    expect(await run(client.upsertObject(), UpsertObjectDefinition.operation, input, context)).toMatchObject({ branch });
    expect(requests.map(request => request.method)).toEqual(methods);
  });
});

describe('updateObject repeat-safe retry', () => {
  it.each([402, 302, 418])('retries HTTP %i, which does not prove a refusal', async status => {
    const { client } = newClient(() => ({ status, body: '{"success":false}' }));
    const input = { objectType: 'deals', objectId: '42', fields: { stage_id: 3 } };
    expect(await run(client.updateObject(), UpdateObjectDefinition.operation, input, new AttemptContext())).toBeInstanceOf(RetryError);
  });
});
