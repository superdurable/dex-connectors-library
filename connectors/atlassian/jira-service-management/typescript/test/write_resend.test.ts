// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { JiraServiceManagementClient } from '../src/client.js';
import { AddCommentDefinition, CreateTicketDefinition, TransitionTicketDefinition, UpdateTicketDefinition } from '../src/generated/definitions.js';
import type { TransitionTicketInput } from '../src/generated/models.js';

const callId = 'ab66bfde-d934-5d47-9e00-bdf172b83392';
const connection = { provider: 'atlassian', name: 'jsm-test' };
const created = '{"issueId":"10042","issueKey":"ITH-42","serviceDeskId":"10","requestTypeId":"25"}';

class AttemptContext {
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'jsm-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  constructor(public attempt = 1, marker?: unknown) { this.marker = marker; }
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

/** A scripted body that sends the status and headers, then breaks the connection. */
const truncatedBody = '<truncated>';
const createdReadBack = '{"issueId":"10042","issueKey":"ITH-42","serviceDeskId":"10","requestTypeId":"25","summary":"Laptop will not boot"}';
type Answer = (request: ProviderRequest) => { status: number; body: string };

function newClient(status: number, body = '{}') {
  return newScriptedClient(() => ({ status, body }));
}

function newScriptedClient(answer: Answer, token = 'jsm-access-token') {
  const requests: ProviderRequest[] = [];
  const credentials = new StaticCredentialProvider([[connection, { accessToken: new SecretString(token), refreshToken: new SecretString(''), oauthClientId: 'client-id', oauthClientSecret: new SecretString('secret') }]]);
  const client = new JiraServiceManagementClient({ endpoint: 'http://127.0.0.1:12345', cloudId: '11223344-a1b2-4b33-8c44-def123456789' }, credentials, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body } = answer(request);
      if (body === truncatedBody) throw new ProviderTransportError(false, status, {});
      return { status, headers: {}, body: Buffer.from(body) };
    },
  });
  return { client, requests };
}

/** Answer the create POST and each read-back GET separately. */
function createdOrReadBack(readStatus: number, readBody: string, writeBody = created, writeStatus = 201): Answer {
  return request => request.method === 'GET' ? { status: readStatus, body: readBody } : { status: writeStatus, body: writeBody };
}

async function run(client: JiraServiceManagementClient, operation: 'createTicket' | 'addComment', context: AttemptContext, maxSendsOverride?: number): Promise<string> {
  const definition = operation === 'createTicket' ? CreateTicketDefinition : AddCommentDefinition;
  const call = { id: callId, connection, operation: definition.operation, context, ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try {
    const result: MutationResult<unknown> = operation === 'createTicket'
      ? await runMutation(client.createTicket(), call, { serviceDeskId: '10', requestTypeId: '25', summary: 'Laptop will not boot' })
      : await runMutation(client.addComment(), call, { issueIdOrKey: 'ITH-42', body: 'Check the warranty first.', isPublic: true });
    return result.branch;
  } catch (error) {
    if (error instanceof RetryError) return 'retry';
    throw error;
  }
}

describe('Jira Service Management bounded resend of unconfirmed writes', () => {
  it('resends an unknown outcome below the cap, then selects uncertain', async () => {
    const { client, requests } = newClient(500);
    const context = new AttemptContext();
    for (; context.attempt < CreateTicketDefinition.maxSends!; context.attempt++) {
      expect(await run(client, 'createTicket', context)).toBe('retry');
      expect(context.marker).toMatchObject({ state: 'claimed', sendCount: context.attempt });
    }
    expect(await run(client, 'createTicket', context)).toBe('uncertain');
    context.attempt++;
    expect(await run(client, 'createTicket', context)).toBe('uncertain');
    expect(requests).toHaveLength(3);
  });

  it('never resends with a send cap of one', async () => {
    const { client, requests } = newClient(409);
    expect(await run(client, 'addComment', new AttemptContext(), 1)).toBe('uncertain');
    expect(requests).toHaveLength(1);
  });

  it('resends on a later attempt and reads back a saved identity instead of resending it', async () => {
    const { client, requests } = newScriptedClient(createdOrReadBack(200, createdReadBack));
    const context = new AttemptContext(2);
    expect(await run(client, 'createTicket', context)).toBe('created');
    expect(JSON.stringify(context.marker)).toContain('"providerId":"10042"');
    context.attempt = 3;
    expect(await run(client, 'createTicket', context)).toBe('created');
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET']);
    const atCap = newClient(201, '{"id":"1001"}');
    expect(await run(atCap.client, 'addComment', new AttemptContext(AddCommentDefinition.maxSends! + 1))).toBe('uncertain');
    expect(atCap.requests).toEqual([]);
  });

  it('retries a transient read-back of a saved identity and never resends it', async () => {
    const context = new AttemptContext();
    const first = newScriptedClient(createdOrReadBack(503, '{}', '{"issueId":"10042","issueKey":"bad key"}'));
    expect(await run(first.client, 'createTicket', context)).toBe('retry');
    context.attempt = 2;
    const second = newScriptedClient(createdOrReadBack(404, '{}'));
    expect(await run(second.client, 'createTicket', context)).toBe('uncertain');
    expect([...first.requests, ...second.requests].map(request => request.method)).toEqual(['POST', 'GET', 'GET']);
  });

  it.each([
    ['createTicket', truncatedBody], ['createTicket', '{"issueKey":"SENTINEL"}'], ['addComment', truncatedBody], ['addComment', '{"id":"SENTINEL"}'],
  ] as const)('never resends an accepted %s whose body is %s', async (operation, body) => {
    const { client, requests } = newClient(201, body);
    expect(await run(client, operation, new AttemptContext())).toBe('uncertain');
    expect(requests).toHaveLength(1);
  });

  it.each(['createTicket', 'addComment'] as const)('passes the fence write-window signal to the %s request', async operation => {
    const { client, requests } = newClient(201, operation === 'createTicket' ? created : '{"id":"1001"}');
    const context = new AttemptContext();
    await run(client, operation, context);
    expect(requests[0]!.signal).toBeDefined();
    expect(requests[0]!.signal).not.toBe(context.cancellationSignal);
  });

  it('saves the provider identity of a confirmed conflict', async () => {
    const { client } = newScriptedClient(createdOrReadBack(200, createdReadBack, '{"issueId":"10042"}', 409));
    const context = new AttemptContext();
    expect(await run(client, 'createTicket', context)).toBe('created');
    expect(JSON.stringify(context.marker)).toContain('"providerId":"10042"');
  });

  it('records no notSent marker for a credential failure after a possible send', async () => {
    const context = new AttemptContext();
    expect(await run(newClient(500).client, 'createTicket', context)).toBe('retry');
    context.attempt = 2;
    expect(await run(newClient(429).client, 'createTicket', context)).toBe('retry');
    const marker = context.marker;
    expect(JSON.stringify(marker)).toContain('"state":"canResend"');
    context.attempt = 3;
    const withoutCredentials = newScriptedClient(() => ({ status: 201, body: created }), '');
    expect(await run(withoutCredentials.client, 'createTicket', context)).toBe('uncertain');
    expect(context.marker).toBe(marker);
    expect(withoutCredentials.requests).toEqual([]);
  });

  it('records canResend on a definite refusal and never resends it', async () => {
    const { client, requests } = newClient(403);
    const context = new AttemptContext();
    expect(await run(client, 'addComment', context)).toBe('providerRejected');
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(requests).toHaveLength(1);
  });

  it('selects uncertain when a resend is refused', async () => {
    const context = new AttemptContext();
    expect(await run(newClient(502).client, 'createTicket', context)).toBe('retry');
    context.attempt = 2;
    expect(await run(newClient(400).client, 'createTicket', context)).toBe('uncertain');
  });

  it('retries an untrusted checkpoint without sending', async () => {
    const { client, requests } = newClient(201, created);
    expect(await run(client, 'createTicket', new AttemptContext(2, { callId: 'another-call', attempt: 1, state: 'claimed' }))).toBe('retry');
    expect(requests).toEqual([]);
  });

  it('treats a refusal status whose body exceeds the limit as an unknown outcome', async () => {
    const context = new AttemptContext();
    expect(await run(newStallingClient(() => ({ status: 400, body: oversizedBody })).client, 'addComment', context)).toBe('retry');
    expect(JSON.stringify(context.marker)).toContain('"state":"claimed"');
  });

  it('never resends a write whose 2xx body the write deadline cut off', async () => {
    const { client, requests } = newStallingClient(() => ({ status: 201, body: stalledBody }));
    expect(await run(client, 'createTicket', new ShortWindowContext())).toBe('uncertain');
    expect(requests).toHaveLength(1);
  });

  it('retries a transient credential failure before any send and records notSent', async () => {
    const context = new AttemptContext();
    const { client, requests } = newStallingClient(() => ({ status: 201, body: created }), failingCredentials(() => new Error('credential broker unavailable')));
    expect(await run(client, 'createTicket', context)).toBe('retry');
    expect(JSON.stringify(context.marker)).toContain('"state":"notSent"');
    expect(requests).toEqual([]);
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

type CredentialSourceForTest = ConstructorParameters<typeof JiraServiceManagementClient>[1];

/** A credential source whose every lookup throws `error`. */
function failingCredentials(error: () => Error): CredentialSourceForTest {
  const resolve = async () => { throw error(); };
  return { resolve, resolveWithRefresh: resolve } as unknown as CredentialSourceForTest;
}

function newStallingClient(answer: Answer, credentials?: CredentialSourceForTest) {
  const requests: ProviderRequest[] = [];
  const source = credentials ?? new StaticCredentialProvider([[connection, { accessToken: new SecretString('jsm-access-token'), refreshToken: new SecretString(''), oauthClientId: 'client-id', oauthClientSecret: new SecretString('secret') }]]);
  const client = new JiraServiceManagementClient({ endpoint: 'http://127.0.0.1:12345', cloudId: '11223344-a1b2-4b33-8c44-def123456789' }, source, {
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

const inProgress = '{"id":"10042","key":"ITH-42","fields":{"status":{"id":"3","name":"In progress","statusCategory":{"key":"indeterminate"}}},"transitions":[{"id":"21","name":"Pending","to":{"id":"1","name":"Waiting for support","statusCategory":{"key":"new"}}}]}';
const waiting = '{"id":"10042","key":"ITH-42","fields":{"status":{"id":"1","name":"Waiting for support","statusCategory":{"key":"new"}}},"transitions":[{"id":"11","name":"Pending","to":{"id":"3","name":"In progress","statusCategory":{"key":"indeterminate"}}}]}';
const pending: TransitionTicketInput = { issueIdOrKey: 'ITH-42', transitionName: 'Pending' };
const transitionPosts = (requests: ProviderRequest[]) => requests.filter(request => request.method === 'POST').length;

async function runTransition(client: JiraServiceManagementClient, context: AttemptContext, input: TransitionTicketInput = pending): Promise<string> {
  const call = { id: callId, connection, operation: TransitionTicketDefinition.operation, context } as unknown as Call;
  try { return (await runMutation(client.transitionTicket(), call, input)).branch; }
  catch (error) { if (error instanceof RetryError) return 'retry'; throw error; }
}

const recordedPending = { callId, attempt: 1, transitionId: '21', transitionName: 'Pending', sourceStatusId: '3', destinationStatusId: '1' };
const resolved = '{"id":"10042","key":"ITH-42","fields":{"status":{"id":"5","name":"Resolved","statusCategory":{"key":"done"}}},"transitions":[{"id":"51","name":"Reopen","to":{"id":"1","name":"Waiting for support","statusCategory":{"key":"new"}}}]}';
const postedTransitionIds = (requests: ProviderRequest[]) => requests.filter(request => request.method === 'POST').map(request => (JSON.parse(Buffer.from(request.body ?? '').toString()) as { transition: { id: string } }).transition.id);

class UnreadableContext extends AttemptContext {
  override hasLastHeartbeatValue(): boolean { throw new Error('heartbeat unavailable'); }
}

describe('Jira Service Management transition with a recorded checkpoint', () => {
  it('records its checkpoint before sending', async () => {
    const { client } = newStallingClient(request => request.method === 'GET' ? { status: 200, body: inProgress } : { status: 502, body: '{}' });
    const context = new AttemptContext();
    expect(await runTransition(client, context)).toBe('retry');
    expect(context.marker).toEqual(recordedPending);
    const unreadable = newStallingClient(() => ({ status: 204, body: '' }));
    expect(await runTransition(unreadable.client, new UnreadableContext(2))).toBe('retry');
    expect(unreadable.requests, 'an unreadable checkpoint never falls back to selecting again').toEqual([]);
  });

  it('sends nothing when a retry finds the recorded destination', async () => {
    const issue = { body: inProgress };
    const { client, requests } = newStallingClient(request => request.method === 'GET' ? { status: 200, body: issue.body } : { status: 502, body: '{}' });
    const context = new AttemptContext();
    expect(await runTransition(client, context)).toBe('retry');
    // Jira applied the toggle late; a resend would move the request back.
    issue.body = waiting;
    context.attempt = 2;
    expect(await runTransition(client, context)).toBe('transitioned');
    expect(transitionPosts(requests)).toBe(1);
  });

  it('resends the recorded transition while the request is still in its source status', async () => {
    // A second Pending would make selecting by name again ambiguous.
    const ambiguous = inProgress.replace('"transitions":[', '"transitions":[{"id":"22","name":"Pending","to":{"id":"5","name":"Resolved","statusCategory":{"key":"done"}}},');
    const { client, requests } = newStallingClient(request => request.method === 'GET' ? { status: 200, body: ambiguous } : { status: 204, body: '' });
    expect(await runTransition(client, new AttemptContext(2, recordedPending))).toBe('transitioned');
    expect(postedTransitionIds(requests)).toEqual(['21']);
  });

  it('selects transitionUnavailable without sending when someone else moved the request', async () => {
    const { client, requests } = newStallingClient(request => request.method === 'GET' ? { status: 200, body: resolved } : { status: 204, body: '' });
    expect(await runTransition(client, new AttemptContext(2, recordedPending))).toBe('transitionUnavailable');
    expect(transitionPosts(requests)).toBe(0);
  });

  it('keeps a refused resend definite', async () => {
    const refusal = '{"errorMessages":[],"errors":{"resolution":"Resolution is required."}}';
    const { client, requests } = newStallingClient(request => request.method === 'GET' ? { status: 200, body: inProgress } : { status: 400, body: refusal });
    expect(await runTransition(client, new AttemptContext(2, recordedPending))).toBe('providerRejected');
    expect(transitionPosts(requests)).toBe(1);
  });

  it('recognizes the destination status on a repeated attempt', async () => {
    const issue = { body: waiting };
    const { client, requests } = newStallingClient(request => request.method === 'GET' ? { status: 200, body: issue.body } : { status: 504, body: '' });
    const move: TransitionTicketInput = { issueIdOrKey: 'ITH-42', transitionId: '11', destinationStatusName: 'In progress' };
    const context = new AttemptContext();
    expect(await runTransition(client, context, move)).toBe('retry');
    issue.body = inProgress;
    context.attempt = 2;
    expect(await runTransition(client, context, move)).toBe('transitioned');
    expect(transitionPosts(requests)).toBe(1);
  });

  it.each([[500, 'retry'], [404, 'notFound']])('retries a transient read and keeps a definite read after a recorded send (HTTP %i)', async (status, branch) => {
    const { client, requests } = newStallingClient(() => ({ status, body: '{}' }));
    expect(await runTransition(client, new AttemptContext(2, recordedPending))).toBe(branch);
    expect(requests).toHaveLength(1);
  });

  it('lets the next attempt send after a rate limit', async () => {
    const statuses = [429, 204];
    const { client, requests } = newStallingClient(request => request.method === 'GET' ? { status: 200, body: inProgress } : { status: statuses.shift()!, body: '' });
    const context = new AttemptContext();
    expect(await runTransition(client, context)).toBe('retry');
    expect(context.marker).toEqual(recordedPending);
    context.attempt = 2;
    expect(await runTransition(client, context)).toBe('transitioned');
    expect(transitionPosts(requests)).toBe(2);
  });

  it('sends on the third attempt after two rate limits', async () => {
    const statuses = [429, 429, 204];
    const { client, requests } = newStallingClient(request => request.method === 'GET' ? { status: 200, body: inProgress } : { status: statuses.shift()!, body: '' });
    const context = new AttemptContext();
    for (const attempt of [1, 2]) {
      context.attempt = attempt;
      expect(await runTransition(client, context)).toBe('retry');
    }
    context.attempt = 3;
    expect(await runTransition(client, context)).toBe('transitioned');
    expect(transitionPosts(requests)).toBe(3);
  });

  it('sends on the third attempt after two pre-send Retries', async () => {
    const answer: Answer = request => request.method === 'GET' ? { status: 200, body: inProgress } : { status: 204, body: '' };
    const unavailable = newStallingClient(answer, failingCredentials(() => new Error('credential broker unavailable')));
    const context = new AttemptContext();
    for (const attempt of [1, 2]) {
      context.attempt = attempt;
      expect(await runTransition(unavailable.client, context)).toBe('retry');
    }
    const { client, requests } = newStallingClient(answer);
    context.attempt = 3;
    expect(await runTransition(client, context)).toBe('transitioned');
    expect(transitionPosts(requests)).toBe(1);
  });

  it('is transitioned when the write deadline cuts a 2xx body', async () => {
    const { client, requests } = newStallingClient(request => request.method === 'GET' ? { status: 200, body: inProgress } : { status: 200, body: stalledBody });
    expect(await runTransition(client, new ShortWindowContext())).toBe('transitioned');
    expect(transitionPosts(requests)).toBe(1);
  });

  it('follows the send history on credential failures', async () => {
    const terminal = failingCredentials(() => new ReauthorizationRequiredError());
    const answer: Answer = () => ({ status: 204, body: '' });
    expect(await runTransition(newStallingClient(answer, terminal).client, new AttemptContext())).toBe('defect');
    expect(await runTransition(newStallingClient(answer, terminal).client, new AttemptContext(2, recordedPending))).toBe('retry');
    const context = new AttemptContext();
    const { client, requests } = newStallingClient(answer, failingCredentials(() => new Error('credential broker unavailable')));
    expect(await runTransition(client, context)).toBe('retry');
    expect(context.marker, 'a transient failure before any send records notSent').toEqual({ callId, attempt: 1, state: 'notSent' });
    expect(requests).toEqual([]);
  });

  it('never selects again after a lost checkpoint', async () => {
    // Dex lost the checkpoint after a possible send; selecting Pending again would undo the move.
    const lost = newStallingClient(request => request.method === 'GET' ? { status: 200, body: waiting } : { status: 204, body: '' });
    expect(await runTransition(lost.client, new AttemptContext(2))).toBe('retry');
    expect(transitionPosts(lost.requests)).toBe(0);
    const notSent = newStallingClient(request => request.method === 'GET' ? { status: 200, body: inProgress } : { status: 204, body: '' });
    expect(await runTransition(notSent.client, new AttemptContext(2, { callId, attempt: 1, state: 'notSent' }))).toBe('transitioned');
    expect(transitionPosts(notSent.requests)).toBe(1);
  });
});

const labelsAndPriority = '{"id":"10042","key":"ITH-42","fields":{"labels":["hardware"],"priority":{"id":"3","name":"Medium"}}}';

async function runUpdate(client: JiraServiceManagementClient): Promise<string> {
  const call = { id: callId, connection, operation: UpdateTicketDefinition.operation, context: new AttemptContext() } as unknown as Call;
  try { return (await runMutation(client.updateTicket(), call, { issueIdOrKey: 'ITH-42', addLabels: ['vip'] })).branch; }
  catch (error) { if (error instanceof RetryError) return 'retry'; throw error; }
}

describe('Jira Service Management updateTicket unknown answers', () => {
  it.each([
    ['read refusal with a cut body', 'GET', 400, truncatedBody],
    ['read refusal echoing the token', 'GET', 403, '{"errorMessages":["jsm-access-token"]}'],
    ['read refusal with an unlisted status', 'GET', 409, '{}'],
    ['unparseable read', 'GET', 200, '{"id":"x"}'],
    ['mistyped labels', 'GET', 200, '{"id":"10042","key":"ITH-42","fields":{"labels":"vip"}}'],
    ['missing issue with a cut body', 'GET', 404, truncatedBody],
    ['write refusal with a cut body', 'PUT', 400, truncatedBody],
    ['write refusal echoing the token', 'PUT', 400, '{"errors":{"priority":"jsm-access-token"}}'],
  ])('retries a %s', async (_name, method, status, body) => {
    const { client } = newScriptedClient(request => request.method === method ? { status, body } : { status: 200, body: labelsAndPriority });
    expect(await runUpdate(client)).toBe('retry');
  });

  it('keeps a readable definite refusal', async () => {
    const { client } = newScriptedClient(request => request.method === 'PUT' ? { status: 400, body: '{"errors":{"labels":"bad"}}' } : { status: 200, body: labelsAndPriority });
    expect(await runUpdate(client)).toBe('providerRejected');
  });
});
