// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { JiraClient } from '../src/client.js';
import { AddCommentDefinition, CreateIssueDefinition, TransitionIssueDefinition } from '../src/generated/definitions.js';
import type { TransitionIssueInput } from '../src/generated/models.js';

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

/** A scripted body that sends the status and headers, then breaks the connection. */
const truncatedBody = '<truncated>';

function newClient(answer: Answer, credentialSource: typeof source = source) {
  const requests: ProviderRequest[] = [];
  const client = new JiraClient({ endpoint: 'http://127.0.0.1', cloudId }, credentialSource, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body } = answer(request);
      if (body === truncatedBody) throw new ProviderTransportError(false, status, {});
      return { status, headers: {}, body: Buffer.from(body) };
    },
  });
  return { client, requests };
}

/** Answer read-backs with nothing and each write with the next scripted response. */
function scriptedWrites(responses: { status: number; body: string }[]): Answer {
  const remaining = [...responses];
  return request => {
    if (request.url.endsWith('/search/jql')) return { status: 200, body: '{"issues":[]}' };
    if (request.method === 'GET') return { status: 200, body: '{"comments":[]}' };
    return remaining.shift()!;
  };
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

  it.each([
    ['createIssue', '{"id":"10043","key":"OPS-43"}', 'created'],
    ['addComment', '{"id":"10500"}', 'added'],
  ] as const)('reads back %s before the third send after a canResend marker', async (operation, created, branch) => {
    const { client, requests } = newClient(scriptedWrites([{ status: 500, body: '{}' }, { status: 429, body: '{}' }, { status: 201, body: created }]));
    const context = new AttemptContext();
    expect(branchOf(await run(client, operation, context))).toBe('retry');
    context.attempt = 2;
    expect(branchOf(await run(client, operation, context))).toBe('retry');
    // Flat and per-call heartbeat formats both carry the marker state.
    expect(JSON.stringify(context.marker)).toContain('"state":"canResend"');
    context.attempt = 3;
    requests.length = 0;
    expect(branchOf(await run(client, operation, context))).toBe(branch);
    expect(requests.map(request => request.method)).toEqual([operation === 'createIssue' ? 'POST' : 'GET', 'POST']);
    expect(new URL(requests[0]!.url).pathname.endsWith(operation === 'createIssue' ? '/search/jql' : '/comment')).toBe(true);
  });

  it.each([
    ['createIssue', truncatedBody], ['createIssue', '{}'], ['addComment', truncatedBody], ['addComment', '{}'],
  ] as const)('never resends an accepted %s whose body is %s', async (operation, body) => {
    const { client, requests } = newClient(emptyReadBackOr(201, body));
    expect(branchOf(await run(client, operation, new AttemptContext()))).toBe('uncertain');
    expect(requests.filter(request => request.method === 'POST' && !request.url.endsWith('/search/jql'))).toHaveLength(1);
  });

  it('selects uncertain when the read-back of an accepted issue without an ID is rate limited', async () => {
    const { client, requests } = newClient(request => request.url.endsWith('/search/jql') ? { status: 429, body: '{}' } : { status: 201, body: '{}' });
    expect(branchOf(await run(client, 'createIssue', new AttemptContext()))).toBe('uncertain');
    expect(posts(requests, '/issue')).toBe(1);
  });

  it('records no notSent marker for a credential failure after a possible send', async () => {
    let mode: 'ready' | 'transient' | 'invalid' = 'ready';
    const resolve = async () => {
      if (mode === 'transient') throw new Error('credential broker unavailable');
      return mode === 'invalid' ? { ...credentials, accessToken: new SecretString('') } : credentials;
    };
    const { client, requests } = newClient(scriptedWrites([{ status: 500, body: '{}' }, { status: 429, body: '{}' }]), { resolve, resolveWithRefresh: resolve });
    const context = new AttemptContext();
    for (const attempt of [1, 2]) {
      context.attempt = attempt;
      expect(branchOf(await run(client, 'createIssue', context))).toBe('retry');
    }
    const marker = context.marker;
    mode = 'transient';
    context.attempt = 3;
    expect(branchOf(await run(client, 'createIssue', context))).toBe('retry');
    expect(context.marker).toBe(marker);
    mode = 'invalid';
    context.attempt = 4;
    expect(branchOf(await run(client, 'createIssue', context))).toBe('uncertain');
    expect(posts(requests, '/issue')).toBe(2);
  });

  it('treats a refusal status whose body exceeds the limit as an unknown outcome', async () => {
    const { client } = newStallingClient(emptyReadBackOr(400, oversizedBody));
    const context = new AttemptContext();
    expect(branchOf(await run(client, 'createIssue', context))).toBe('retry');
    expect(JSON.stringify(context.marker)).toContain('"state":"claimed"');
  });

  it('never resends a write whose 2xx body the write deadline cut off', async () => {
    const { client, requests } = newStallingClient(emptyReadBackOr(201, stalledBody));
    expect(branchOf(await run(client, 'createIssue', new ShortWindowContext()))).toBe('uncertain');
    expect(posts(requests, '/issue')).toBe(1);
  });
});

/** A scripted body that sends the status, then stalls until the write window aborts the request. */
const stalledBody = '<stalled>';
/** A scripted body larger than the error-body limit. */
const oversizedBody = '<oversized>';

/** A first attempt whose write window ends long before a stalled body would finish. */
class ShortWindowContext extends AttemptContext {
  override readonly deadline = new Date(Date.now() + 1_500);
}

function newStallingClient(answer: Answer, credentialSource: typeof source = source) {
  const requests: ProviderRequest[] = [];
  const client = new JiraClient({ endpoint: 'http://127.0.0.1', cloudId }, credentialSource, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body } = answer(request);
      if (body === stalledBody) {
        await new Promise(resolve => request.signal?.addEventListener('abort', resolve, { once: true }));
        throw new ProviderTransportError(false, status, {});
      }
      if (body === oversizedBody) throw new BodyTooLargeError(status, {});
      return { status, headers: {}, body: Buffer.from(body) };
    },
  });
  return { client, requests };
}

const inProgressIssue = '{"id":"10042","key":"OPS-42","fields":{"status":{"id":"3","name":"In Progress","statusCategory":{"key":"indeterminate"}}},"transitions":[{"id":"21","name":"Stop Progress","to":{"id":"1","name":"To Do","statusCategory":{"key":"new"}}}]}';
const toDoIssue = '{"id":"10042","key":"OPS-42","fields":{"status":{"id":"1","name":"To Do","statusCategory":{"key":"new"}}},"transitions":[{"id":"21","name":"Stop Progress","to":{"id":"3","name":"In Progress","statusCategory":{"key":"indeterminate"}}}]}';
const stopProgress = { issueIdOrKey: 'OPS-42', transitionName: 'Stop Progress' };

async function runTransition(client: JiraClient, context: AttemptContext, input: TransitionIssueInput = stopProgress): Promise<MutationResult<unknown> | RetryError> {
  const call = { id: callId, connection: { provider: 'atlassian', name: 'jira-test' }, operation: TransitionIssueDefinition.operation, context } as unknown as Call;
  try { return await runMutation(client.transitionIssue(), call, input); }
  catch (error) { if (error instanceof RetryError) return error; throw error; }
}

describe('Jira transition behind the single-dispatch fence', () => {
  it('never transitions again after an unknown outcome', async () => {
    const issue = { body: inProgressIssue };
    const { client, requests } = newStallingClient(request => request.method === 'GET' ? { status: 200, body: issue.body } : { status: 502, body: '{}' });
    const context = new AttemptContext();
    expect(branchOf(await runTransition(client, context))).toBe('retry');
    // Jira applied the toggle late; a resend would move the issue back.
    issue.body = toDoIssue;
    for (const attempt of [2, 3]) {
      context.attempt = attempt;
      expect(branchOf(await runTransition(client, context))).toBe('retry');
    }
    expect(posts(requests, '/transitions')).toBe(1);
  });

  it('recognizes the destination status on a repeated attempt', async () => {
    const issue = { body: toDoIssue };
    const { client, requests } = newStallingClient(request => request.method === 'GET' ? { status: 200, body: issue.body } : { status: 504, body: '' });
    const context = new AttemptContext();
    const move = { issueIdOrKey: 'OPS-42', transitionId: '21', destinationStatusName: 'In Progress' };
    expect(branchOf(await runTransition(client, context, move))).toBe('retry');
    issue.body = inProgressIssue;
    context.attempt = 2;
    expect(branchOf(await runTransition(client, context, move))).toBe('transitioned');
    expect(posts(requests, '/transitions')).toBe(1);
  });

  it.each([[404, '{}'], [200, '{"id":"x"}'], [403, '{}']])('never selects a failure branch after a possible send (HTTP %i)', async (status, body) => {
    const { client, requests } = newStallingClient(() => ({ status, body }));
    expect(branchOf(await runTransition(client, new AttemptContext(2)))).toBe('retry');
    expect(requests).toHaveLength(1);
  });

  it('lets the next attempt send after a rate limit', async () => {
    const statuses = [429, 204];
    const { client, requests } = newStallingClient(request => request.method === 'GET' ? { status: 200, body: inProgressIssue } : { status: statuses.shift()!, body: '' });
    const context = new AttemptContext();
    expect(branchOf(await runTransition(client, context))).toBe('retry');
    expect(JSON.stringify(context.marker)).toContain('"state":"canResend"');
    context.attempt = 2;
    expect(branchOf(await runTransition(client, context))).toBe('transitioned');
    expect(posts(requests, '/transitions')).toBe(2);
  });

  it('sends on the third attempt after two rate limits', async () => {
    const statuses = [429, 429, 204];
    const { client, requests } = newStallingClient(request => request.method === 'GET' ? { status: 200, body: inProgressIssue } : { status: statuses.shift()!, body: '' });
    const context = new AttemptContext();
    for (const attempt of [1, 2]) {
      context.attempt = attempt;
      expect(branchOf(await runTransition(client, context))).toBe('retry');
    }
    context.attempt = 3;
    expect(branchOf(await runTransition(client, context))).toBe('transitioned');
    expect(posts(requests, '/transitions')).toBe(3);
  });

  it('sends on the third attempt after two pre-send Retries', async () => {
    let isReady = false;
    const resolve = async () => { if (!isReady) throw new Error('credential broker unavailable'); return credentials; };
    const { client, requests } = newStallingClient(request => request.method === 'GET' ? { status: 200, body: inProgressIssue } : { status: 204, body: '' }, { resolve, resolveWithRefresh: resolve });
    const context = new AttemptContext();
    for (const attempt of [1, 2]) {
      context.attempt = attempt;
      expect(branchOf(await runTransition(client, context))).toBe('retry');
    }
    isReady = true;
    context.attempt = 3;
    expect(branchOf(await runTransition(client, context))).toBe('transitioned');
    expect(posts(requests, '/transitions')).toBe(1);
  });

  it('is transitioned when the write deadline cuts a 2xx body', async () => {
    const { client, requests } = newStallingClient(request => request.method === 'GET' ? { status: 200, body: inProgressIssue } : { status: 200, body: stalledBody });
    expect(branchOf(await runTransition(client, new ShortWindowContext()))).toBe('transitioned');
    expect(posts(requests, '/transitions')).toBe(1);
  });

  it('follows the send history on credential failures', async () => {
    let mode: 'transient' | 'invalid' = 'invalid';
    const resolve = async () => {
      if (mode === 'transient') throw new Error('credential broker unavailable');
      return { ...credentials, accessToken: new SecretString('') };
    };
    const { client, requests } = newStallingClient(() => ({ status: 204, body: '' }), { resolve, resolveWithRefresh: resolve });
    expect(branchOf(await runTransition(client, new AttemptContext()))).toBe('defect');
    expect(branchOf(await runTransition(client, new AttemptContext(2)))).toBe('retry');
    mode = 'transient';
    const context = new AttemptContext();
    expect(branchOf(await runTransition(client, context))).toBe('retry');
    expect(JSON.stringify(context.marker)).toContain('"state":"notSent"');
    expect(requests).toEqual([]);
  });
});
