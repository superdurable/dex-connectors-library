// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { JiraServiceManagementClient } from '../src/client.js';
import { AddCommentDefinition, CreateTicketDefinition } from '../src/generated/definitions.js';

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
});
