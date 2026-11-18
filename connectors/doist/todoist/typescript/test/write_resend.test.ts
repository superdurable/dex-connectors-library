// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { ProviderStalledError, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { TodoistClient } from '../src/client.js';
import { CreateTaskDefinition, DeleteTaskDefinition, UpdateTaskDefinition } from '../src/generated/definitions.js';
import { AttemptContext } from './attempt-context.js';

const callId = '51afb24a-372a-5f2f-927c-a0493cb99a0b';
const task = JSON.stringify({ id: 'task-123', project_id: 'project-456', content: 'Title', description: '', checked: false });
type Operation = 'createTask' | 'updateTaskContent' | 'deleteTask';

function newClient(answer: (request: ProviderRequest) => ProviderResponse, token = 'todoist-test-token') {
  const requests: ProviderRequest[] = [];
  const client = new TodoistClient({ baseUrl: 'http://127.0.0.1:12345/api/v1' }, { resolve: async () => ({ apiToken: new SecretString(token) }) }, {
    send: async (request: ProviderRequest) => { requests.push(request); return answer(request); },
  });
  return { client, requests };
}

function status(code: number, body = ''): (request: ProviderRequest) => ProviderResponse {
  return () => ({ status: code, headers: {}, body: Buffer.from(body) });
}

async function run(client: TodoistClient, operation: Operation, context: AttemptContext, maxSendsOverride?: number): Promise<string> {
  const definition = operation === 'createTask' ? CreateTaskDefinition : operation === 'updateTaskContent' ? UpdateTaskDefinition : DeleteTaskDefinition;
  const call = { id: callId, connection: { provider: 'todoist', name: 'todoist' }, operation: definition.operation, context, ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try {
    const result: MutationResult<unknown> = operation === 'createTask'
      ? await runMutation(client.createTask(), call, { content: 'Title' })
      : operation === 'updateTaskContent'
        ? await runMutation(client.updateTaskContent(), call, { taskId: 'task-123', content: 'Title' })
        : await runMutation(client.deleteTask(), call, { taskId: 'task-123' });
    return result.branch;
  } catch (error) {
    if (error instanceof RetryError) return 'retry';
    throw error;
  }
}

describe('Todoist bounded resend of unconfirmed creates', () => {
  it('resends an unknown outcome below the cap, then selects uncertain', async () => {
    const { client, requests } = newClient(status(500));
    const context = new AttemptContext();
    for (; context.attempt < CreateTaskDefinition.maxSends!; context.attempt++) {
      expect(await run(client, 'createTask', context)).toBe('retry');
      expect(context.marker).toMatchObject({ state: 'claimed', sendCount: context.attempt });
    }
    expect(await run(client, 'createTask', context)).toBe('uncertain');
    context.attempt++;
    expect(await run(client, 'createTask', context)).toBe('uncertain');
    expect(requests).toHaveLength(3);
    expect(new Set(requests.map(request => request.headers?.['X-Request-ID'])).size).toBe(1);
  });

  it('never resends with a send cap of one', async () => {
    const { client, requests } = newClient(status(409));
    expect(await run(client, 'createTask', new AttemptContext(), 1)).toBe('uncertain');
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain for a 2xx without a task ID and never resends it', async () => {
    const { client, requests } = newClient(status(200, '{"ok":true}'));
    expect(await run(client, 'createTask', new AttemptContext())).toBe('uncertain');
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain when a 2xx body breaks after its headers and never resends it', async () => {
    for (const failure of [new ProviderTransportError(false, 200, {}), new ProviderStalledError(201, {})]) {
      const { client, requests } = newClient(() => { throw failure; });
      const context = new AttemptContext();
      expect(await run(client, 'createTask', context)).toBe('uncertain');
      expect(context.marker).toMatchObject({ state: 'claimed' });
      expect(requests).toHaveLength(1);
    }
  });

  it('passes the Fence write-window signal to the creation request', async () => {
    const { client, requests } = newClient(status(200, task));
    const context = new AttemptContext();
    expect(await run(client, 'createTask', context)).toBe('created');
    expect(requests[0]!.signal).toBeDefined();
    expect(requests[0]!.signal).not.toBe(context.cancellationSignal);
  });

  it.each([429, 500, 503])('retries a %i read-back after acceptance and reads the saved task ID again', async code => {
    const reads = [code, 200];
    const { client, requests } = newClient(request => request.method === 'GET' ? status(reads.shift()!, task)(request) : status(200, '{"id":"task-123"}')(request));
    const context = new AttemptContext();
    expect(await run(client, 'createTask', context)).toBe('retry');
    expect(context.marker).toMatchObject({ providerId: 'task-123' });
    context.attempt = 2;
    expect(await run(client, 'createTask', context)).toBe('created');
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET', 'GET']);
  });

  it('selects uncertain for a transient read-back when the task ID could not be saved', async () => {
    const { client, requests } = newClient(request => request.method === 'GET' ? status(503)(request) : status(200, '{"id":"task-123"}')(request));
    const context = new AttemptContext();
    const recordHeartbeat = context.recordHeartbeat.bind(context);
    context.recordHeartbeat = async (value: unknown) => {
      if (JSON.stringify(value).includes('task-123')) throw new Error('heartbeat lost');
      await recordHeartbeat(value);
    };
    expect(await run(client, 'createTask', context)).toBe('uncertain');
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET']);
  });

  it('keeps counting toward the cap after a rate-limited resend', async () => {
    const codes = [502, 429, 502, 502];
    const { client, requests } = newClient(request => status(codes.shift()!)(request));
    const context = new AttemptContext();
    for (; context.attempt <= 3; context.attempt++) expect(await run(client, 'createTask', context)).toBe('retry');
    expect(await run(client, 'createTask', context)).toBe('uncertain');
    expect(requests).toHaveLength(4);
  });

  it('never records notSent for a credential failure after a rate-limited resend', async () => {
    const codes = [502, 429];
    const { client } = newClient(request => status(codes.shift()!)(request));
    const context = new AttemptContext();
    for (; context.attempt <= 2; context.attempt++) expect(await run(client, 'createTask', context)).toBe('retry');
    const before = context.marker;
    const invalid = newClient(status(200, task), 'bad\ntoken');
    expect(await run(invalid.client, 'createTask', context)).toBe('uncertain');
    expect(context.marker).toBe(before);
    expect(invalid.requests).toHaveLength(0);
  });

  it('reads back a saved task ID and never resends it', async () => {
    let readStatus = 200;
    const { client, requests } = newClient(request => request.method === 'GET' ? status(readStatus, task)(request) : status(200, task)(request));
    const context = new AttemptContext();
    expect(await run(client, 'createTask', context)).toBe('created');
    expect(context.marker).toMatchObject({ state: 'claimed', providerId: 'task-123' });
    context.attempt = 2;
    expect(await run(client, 'createTask', context)).toBe('created');
    readStatus = 500;
    context.attempt = 3;
    expect(await run(client, 'createTask', context)).toBe('retry');
    readStatus = 404;
    expect(await run(client, 'createTask', context)).toBe('uncertain');
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET', 'GET', 'GET']);
  });

  it('records canResend on a definite refusal and never resends it', async () => {
    const { client, requests } = newClient(status(400));
    const context = new AttemptContext();
    expect(await run(client, 'createTask', context)).toBe('providerRejected');
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain for a refusal or defect after a possible send', async () => {
    let code = 502;
    const { client, requests } = newClient(request => status(code)(request));
    const context = new AttemptContext();
    expect(await run(client, 'createTask', context)).toBe('retry');
    code = 403;
    context.attempt = 2;
    expect(await run(client, 'createTask', context)).toBe('uncertain');
    const invalid = newClient(status(200, task), 'bad\ntoken');
    context.attempt = 3;
    expect(await run(invalid.client, 'createTask', context)).toBe('uncertain');
    expect(requests).toHaveLength(2);
    expect(invalid.requests).toHaveLength(0);
  });

  it('retries without sending when the checkpoint is untrusted', async () => {
    const { client, requests } = newClient(status(200, task));
    const foreign = { callId: 'fed5a116-c5f6-53ac-81b1-a823fb443a88', attempt: 1, state: 'claimed' };
    expect(await run(client, 'createTask', new AttemptContext(2, foreign))).toBe('retry');
    expect(requests).toHaveLength(0);
  });
});

describe('Todoist safe writes', () => {
  it.each([
    ['updateTaskContent', 302], ['updateTaskContent', 405], ['updateTaskContent', 409], ['updateTaskContent', 410],
    ['deleteTask', 302], ['deleteTask', 405], ['deleteTask', 409], ['deleteTask', 410],
  ] as const)('%s retries HTTP %i and rejects only a definite refusal', async (operation, code) => {
    expect(await run(newClient(status(code)).client, operation, new AttemptContext())).toBe('retry');
    expect(await run(newClient(status(422)).client, operation, new AttemptContext())).toBe('providerRejected');
  });
});
