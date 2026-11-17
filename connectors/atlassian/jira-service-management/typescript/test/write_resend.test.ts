// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
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

function newClient(status: number, body = '{}') {
  const requests: ProviderRequest[] = [];
  const credentials = new StaticCredentialProvider([[connection, { accessToken: new SecretString('jsm-access-token'), refreshToken: new SecretString(''), oauthClientId: 'client-id', oauthClientSecret: new SecretString('secret') }]]);
  const client = new JiraServiceManagementClient({ endpoint: 'http://127.0.0.1:12345', cloudId: '11223344-a1b2-4b33-8c44-def123456789' }, credentials, {
    send: async (request: ProviderRequest) => { requests.push(request); return { status, headers: {}, body: Buffer.from(body) }; },
  });
  return { client, requests };
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

  it('resends on a later attempt and never resends a saved identity', async () => {
    const { client, requests } = newClient(201, created);
    const context = new AttemptContext(2);
    expect(await run(client, 'createTicket', context)).toBe('created');
    expect(context.marker).toMatchObject({ providerId: '10042' });
    context.attempt = 3;
    expect(await run(client, 'createTicket', context)).toBe('uncertain');
    expect(await run(newClient(201, '{"id":"1001"}').client, 'addComment', new AttemptContext(AddCommentDefinition.maxSends! + 1))).toBe('uncertain');
    expect(requests).toHaveLength(1);
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
