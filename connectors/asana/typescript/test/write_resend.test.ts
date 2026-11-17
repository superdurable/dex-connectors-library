// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { Client } from '../src/client.js';
import { AddCommentDefinition, CreateTaskDefinition } from '../src/generated/definitions.js';

const callId = '6072a0ce-9ddb-5937-8998-675e48cfa406';
const taskId = '1204567890123456';
const created = `{"data":{"gid":"${taskId}"}}`;

class AttemptContext {
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'asana-resend-flow';
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
  const client = new Client({ endpoint: 'http://127.0.0.1:12345/api/1.0' }, { resolve: async () => ({ accessToken: new SecretString('token') }) }, {
    send: async (request: ProviderRequest) => { requests.push(request); return { status, headers: {}, body: Buffer.from(body) }; },
  });
  return { client, requests };
}

async function run(client: Client, operation: 'createTask' | 'addComment', context: AttemptContext, maxSendsOverride?: number): Promise<string> {
  const definition = operation === 'createTask' ? CreateTaskDefinition : AddCommentDefinition;
  const call = { id: callId, connection: { provider: 'asana', name: 'asana-test' }, operation: definition.operation, context, ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try {
    const result: MutationResult<unknown> = operation === 'createTask'
      ? await runMutation(client.createTask(), call, { name: 'Replace badge reader', projectId: '1201000000000001' })
      : await runMutation(client.addComment(), call, { taskId, text: 'Approved.' });
    return result.branch;
  } catch (error) {
    if (error instanceof RetryError) return 'retry';
    throw error;
  }
}

describe('Asana bounded resend of unconfirmed writes', () => {
  it('resends an unknown outcome below the cap, then selects uncertain', async () => {
    const { client, requests } = newClient(500);
    const context = new AttemptContext();
    for (; context.attempt < CreateTaskDefinition.maxSends!; context.attempt++) {
      expect(await run(client, 'createTask', context)).toBe('retry');
      expect(context.marker).toMatchObject({ state: 'claimed', sendCount: context.attempt });
    }
    expect(await run(client, 'createTask', context)).toBe('uncertain');
    context.attempt++;
    expect(await run(client, 'createTask', context)).toBe('uncertain');
    expect(requests).toHaveLength(3);
  });

  it('never resends with a send cap of one', async () => {
    const { client, requests } = newClient(409);
    expect(await run(client, 'addComment', new AttemptContext(), 1)).toBe('uncertain');
    expect(requests).toHaveLength(1);
  });

  it('reads a saved identity back instead of resending', async () => {
    const posted = newClient(201, created);
    const context = new AttemptContext(2);
    expect(await run(posted.client, 'createTask', context)).toBe('created');
    expect(context.marker).toMatchObject({ providerId: taskId });
    context.attempt = 3;
    const readBack = newClient(200, created);
    expect(await run(readBack.client, 'createTask', context)).toBe('created');
    expect(readBack.requests.map(request => request.method)).toEqual(['GET']);
    expect(await run(newClient(201, created).client, 'addComment', new AttemptContext(AddCommentDefinition.maxSends! + 1))).toBe('uncertain');
  });

  it('records canResend on a definite refusal and never resends it', async () => {
    const { client, requests } = newClient(400, '{"errors":[{"message":"bad"}]}');
    const context = new AttemptContext();
    expect(await run(client, 'createTask', context)).toBe('providerRejected');
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain when a resend is refused', async () => {
    const context = new AttemptContext();
    expect(await run(newClient(502).client, 'addComment', context)).toBe('retry');
    context.attempt = 2;
    expect(await run(newClient(403).client, 'addComment', context)).toBe('uncertain');
  });

  it('retries an untrusted checkpoint without sending', async () => {
    const { client, requests } = newClient(201, created);
    expect(await run(client, 'createTask', new AttemptContext(2, { callId: 'another-call', attempt: 1, state: 'claimed' }))).toBe('retry');
    expect(requests).toEqual([]);
  });
});
