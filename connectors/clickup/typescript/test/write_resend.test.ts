// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ClickUpClient } from '../src/client.js';
import { AddCommentDefinition, CreateTaskDefinition } from '../src/generated/definitions.js';

const callId = 'bef06aa7-8066-5d69-a408-c3d4547c0405';
const listId = '901410000001';
const taskId = '86b2x4k7q';
const taskName = 'Escalation: Fix login [86b2x4k00]';
const firstAttemptAt = new Date('2026-01-01T00:00:00Z');

class AttemptContext {
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'clickup-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly firstAttemptAt = firstAttemptAt;
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  constructor(public attempt = 1, marker?: unknown) { this.marker = marker; }
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

type Responder = (request: ProviderRequest, index: number) => ProviderResponse;

function json(status: number, value: unknown): ProviderResponse { return { status, headers: {}, body: Buffer.from(JSON.stringify(value)) }; }
function task(id: string, parent: string | null = null): Record<string, unknown> {
  return { id, name: taskName, status: { status: 'open', type: 'open' }, date_created: String(firstAttemptAt.getTime() + 1_000), list: { id: listId }, parent };
}

function newClient(respond: Responder) {
  const requests: ProviderRequest[] = [];
  const client = new ClickUpClient({}, { resolve: async () => ({ apiToken: new SecretString('pk_4411723_TESTTOKEN') }) }, {
    send: async (request: ProviderRequest) => { requests.push(request); return respond(request, requests.length - 1); },
  });
  return { client, requests };
}

async function run(client: ClickUpClient, operation: 'createTask' | 'addComment', context: AttemptContext): Promise<MutationResult<unknown> | 'retry'> {
  const definition = operation === 'createTask' ? CreateTaskDefinition : AddCommentDefinition;
  const call = { id: callId, connection: { provider: 'clickup', name: 'clickup-test' }, operation: definition.operation, context } as unknown as Call;
  try {
    return operation === 'createTask'
      ? await runMutation(client.createTask(), call, { listId, name: taskName })
      : await runMutation(client.addComment(), call, { taskId, text: 'Escalated.' });
  } catch (error) {
    if (error instanceof RetryError) return 'retry';
    throw error;
  }
}
function branchOf(result: MutationResult<unknown> | 'retry'): string { return result === 'retry' ? result : result.branch; }
function methods(requests: ProviderRequest[]): string[] { return requests.map(request => request.method); }

describe('ClickUp read-back and bounded resend of unknown writes', () => {
  it('reads back, resends below the cap, and selects uncertain at the cap', async () => {
    const { client, requests } = newClient(request => request.method === 'POST' ? json(500, { ECODE: 'SERVER_001' }) : json(200, { tasks: [], last_page: true }));
    const context = new AttemptContext();
    for (; context.attempt <= 3; context.attempt++) {
      expect(await run(client, 'createTask', context)).toBe('retry');
      expect(context.marker).toMatchObject({ state: 'claimed', sendCount: context.attempt });
    }
    expect(branchOf(await run(client, 'createTask', context))).toBe('uncertain');
    expect(methods(requests)).toEqual(['POST', 'GET', 'POST', 'GET', 'POST', 'GET']);
    expect(new URL(requests[1]!.url).searchParams.get('date_created_gt')).toBe(String(firstAttemptAt.getTime() - 300_000));
  });

  it.each([
    { tasks: [task(taskId)], branch: 'created', sent: ['POST', 'GET'] },
    { tasks: [task(taskId), task('86b2x4k7r')], branch: 'uncertain', sent: ['POST', 'GET'] },
    { tasks: [task(taskId, '86b2x4k99')], branch: 'created', sent: ['POST', 'GET', 'POST'] },
  ])('reports one match, never resends beside several, and resends when none matches: $branch $sent', async ({ tasks, branch, sent }) => {
    const { client, requests } = newClient((request, index) => request.method === 'GET' ? json(200, { tasks, last_page: true })
      : index === 0 ? json(502, { ECODE: 'GATEWAY_1' }) : json(200, task('86b2x4k7s')));
    const context = new AttemptContext();
    expect(await run(client, 'createTask', context)).toBe('retry');
    context.attempt = 2;
    expect(branchOf(await run(client, 'createTask', context))).toBe(branch);
    expect(methods(requests)).toEqual(sent);
  });

  it('records canResend for a definite refusal of a first send and never resends it', async () => {
    const { client, requests } = newClient(() => json(400, { ECODE: 'ITEM_015' }));
    const context = new AttemptContext();
    expect(branchOf(await run(client, 'createTask', context))).toBe('providerRejected');
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it.each([409, 410, 425, 308])('treats HTTP %i as an unknown outcome', async status => {
    const { client } = newClient(() => json(status, { ECODE: 'ITEM_027' }));
    const context = new AttemptContext();
    expect(await run(client, 'createTask', context)).toBe('retry');
    expect(context.marker).toMatchObject({ state: 'claimed' });
  });

  it.each([429, 503])('resends without read-back after HTTP %i', async status => {
    const { client, requests } = newClient((_, index) => index === 0 ? json(status, { ECODE: 'RATE_001' }) : json(200, task(taskId)));
    const context = new AttemptContext();
    expect(await run(client, 'createTask', context)).toBe('retry');
    expect(context.marker).toMatchObject({ state: 'canResend' });
    context.attempt = 2;
    expect(branchOf(await run(client, 'createTask', context))).toBe('created');
    expect(methods(requests)).toEqual(['POST', 'POST']);
  });

  it('retries without sending when the checkpoint cannot be trusted', async () => {
    const { client, requests } = newClient(() => { throw new Error('request sent'); });
    const context = new AttemptContext(2, { clickupDispatchedCallId: callId, clickupDispatchedAt: 1 });
    expect(await run(client, 'createTask', context)).toBe('retry');
    expect(requests).toHaveLength(0);
  });

  it.each([{ status: 200, branch: 'created' }, { status: 404, branch: 'uncertain' }])('reads an accepted create back by ID: HTTP $status', async ({ status, branch }) => {
    const { client, requests } = newClient(() => json(status, task(taskId)));
    const context = new AttemptContext(2, { callId, attempt: 1, state: 'claimed', providerId: taskId, sendCount: 1 });
    expect(branchOf(await run(client, 'createTask', context))).toBe(branch);
    expect(requests.map(request => `${request.method} ${new URL(request.url).pathname}`)).toEqual([`GET /api/v2/task/${taskId}`]);
  });

  it('selects uncertain for an unknown comment only at the cap', async () => {
    const { client, requests } = newClient(request => request.method === 'POST' ? json(500, { ECODE: 'SERVER_001' }) : json(200, { comments: [] }));
    const context = new AttemptContext();
    for (; context.attempt <= 3; context.attempt++) expect(await run(client, 'addComment', context)).toBe('retry');
    expect(branchOf(await run(client, 'addComment', context))).toBe('uncertain');
    expect(methods(requests).filter(method => method === 'POST')).toHaveLength(3);
  });

  it('matches an accepted comment only by its saved ID', async () => {
    const { client, requests } = newClient(() => json(200, { comments: [{ id: '700001', comment_text: 'Edited text', date: '1767225603000' }] }));
    const context = new AttemptContext(2, { callId, attempt: 1, state: 'claimed', providerId: '700001', sendCount: 1 });
    expect(branchOf(await run(client, 'addComment', context))).toBe('added');
    expect(methods(requests)).toEqual(['GET']);
  });
});
