// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { JiraClient } from '../src/client.js';
import { AddCommentDefinition, CreateIssueDefinition } from '../src/generated/definitions.js';

const callId = '62d8b09d-d5a4-53c5-991a-4adc7a1ae5cd';
const cloudId = '11223344-a1b2-4b33-8c44-def123456789';
const credentials = { oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret'), accessToken: new SecretString('jira-access-token'), refreshToken: new SecretString('') };
const source = { resolve: async () => credentials, resolveWithRefresh: async () => credentials };

class AttemptContext {
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'jira-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  constructor(public attempt = 1, marker?: unknown, private readonly isReadFailing = false) { this.marker = marker; }
  hasLastHeartbeatValue(): boolean { if (this.isReadFailing) throw new Error('heartbeat unavailable'); return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

type Answer = (request: ProviderRequest) => { status: number; body: string };

function emptyReadBackOr(status: number, body: string): Answer {
  return request => {
    if (request.url.endsWith('/search/jql')) return { status: 200, body: '{"issues":[]}' };
    if (request.method === 'GET') return { status: 200, body: '{"comments":[]}' };
    return { status, body };
  };
}

function newClient(answer: Answer) {
  const requests: ProviderRequest[] = [];
  const client = new JiraClient({ endpoint: 'http://127.0.0.1', cloudId }, source, {
    send: async (request: ProviderRequest) => { requests.push(request); const { status, body } = answer(request); return { status, headers: {}, body: Buffer.from(body) }; },
  });
  return { client, requests };
}

async function run(client: JiraClient, operation: 'createIssue' | 'addComment', context: AttemptContext, maxSendsOverride?: number): Promise<MutationResult<unknown> | RetryError> {
  const definition = operation === 'createIssue' ? CreateIssueDefinition : AddCommentDefinition;
  const call = { id: callId, connection: { provider: 'atlassian', name: 'jira-test' }, operation: definition.operation, context, ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try {
    if (operation === 'createIssue') return await runMutation(client.createIssue(), call, { projectKey: 'OPS', issueTypeName: 'Task', summary: 'Fire panel wiring' });
    return await runMutation(client.addComment(), call, { issueIdOrKey: 'OPS-43', body: 'Triage started.' });
  } catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

const posts = (requests: ProviderRequest[], suffix: string) => requests.filter(request => request.method === 'POST' && request.url.endsWith(suffix)).length;
const branchOf = (result: MutationResult<unknown> | RetryError) => result instanceof RetryError ? 'retry' : result.branch;

describe('Jira bounded resend of unconfirmed writes', () => {
  it('resends an unknown issue outcome below the cap, then selects uncertain', async () => {
    const { client, requests } = newClient(emptyReadBackOr(500, '{}'));
    const context = new AttemptContext();
    for (; context.attempt < CreateIssueDefinition.maxSends!; context.attempt++) {
      expect(branchOf(await run(client, 'createIssue', context))).toBe('retry');
      expect(context.marker).toMatchObject({ state: 'claimed', sendCount: context.attempt });
    }
    expect(branchOf(await run(client, 'createIssue', context))).toBe('uncertain');
    context.attempt++;
    expect(branchOf(await run(client, 'createIssue', context))).toBe('uncertain');
    expect(posts(requests, '/issue')).toBe(3);
  });

  it('never resends a comment with a send cap of one', async () => {
    const { client, requests } = newClient(emptyReadBackOr(409, '{}'));
    expect(branchOf(await run(client, 'addComment', new AttemptContext(), 1))).toBe('uncertain');
    expect(posts(requests, '/comment')).toBe(1);
  });

  it('reads back before a resend and selects uncertain at the cap without sending', async () => {
    const created = newClient(emptyReadBackOr(201, '{"id":"10043","key":"OPS-43"}'));
    expect(branchOf(await run(created.client, 'createIssue', new AttemptContext(2)))).toBe('created');
    expect(created.requests.map(request => new URL(request.url).pathname.split('/').pop())).toEqual(['jql', 'issue']);
    const atCap = newClient(emptyReadBackOr(201, '{"id":"10500"}'));
    expect(branchOf(await run(atCap.client, 'addComment', new AttemptContext(AddCommentDefinition.maxSends! + 1)))).toBe('uncertain');
    expect(posts(atCap.requests, '/comment')).toBe(0);
  });

  it('records canResend on a definite refusal and never resends it', async () => {
    const { client, requests } = newClient(emptyReadBackOr(400, '{"errors":{"summary":"Summary is required."}}'));
    const context = new AttemptContext();
    expect(branchOf(await run(client, 'createIssue', context))).toBe('providerRejected');
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(posts(requests, '/issue')).toBe(1);
  });

  it('selects uncertain when a resend is refused', async () => {
    let isFirstPost = true;
    const { client, requests } = newClient(request => {
      if (request.method !== 'POST' || !request.url.endsWith('/issue')) return { status: 200, body: '{"issues":[]}' };
      const status = isFirstPost ? 502 : 403; isFirstPost = false;
      return { status, body: '{}' };
    });
    const context = new AttemptContext();
    expect(branchOf(await run(client, 'createIssue', context))).toBe('retry');
    context.attempt = 2;
    expect(branchOf(await run(client, 'createIssue', context))).toBe('uncertain');
    expect(posts(requests, '/issue')).toBe(2);
  });

  it('retries an untrusted checkpoint without sending', async () => {
    const { client, requests } = newClient(emptyReadBackOr(201, '{}'));
    expect(branchOf(await run(client, 'createIssue', new AttemptContext(2, { callId: 'another-call', attempt: 1, state: 'claimed' })))).toBe('retry');
    expect(branchOf(await run(client, 'addComment', new AttemptContext(2, undefined, true)))).toBe('retry');
    expect(requests).toEqual([]);
  });
});
