// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { ConfluenceClient } from '../src/client.js';
import { AddCommentDefinition } from '../src/generated/definitions.js';

const callId = '1d4a9b66-087a-52c9-aee1-005513d3b6ac';
const connection = { provider: 'atlassian', name: 'confluence-site' };

class AttemptContext {
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'confluence-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly firstAttemptAt = new Date();
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  constructor(public attempt = 1, marker?: unknown) { this.marker = marker; }
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

function newClient(postStatus: number) {
  const requests: ProviderRequest[] = [];
  const credentials = new StaticCredentialProvider([[connection, { oauthClientId: 'client', oauthClientSecret: new SecretString('secret'), accessToken: new SecretString('token'), refreshToken: new SecretString('') }]]);
  const client = new ConfluenceClient({ endpoint: 'http://127.0.0.1:12345', cloudId: '11223344-a1b2-4b33-8c44-def123456789' }, credentials, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      if (request.method === 'GET') return { status: 200, headers: {}, body: Buffer.from('{"results":[]}') };
      return { status: postStatus, headers: {}, body: Buffer.from(postStatus < 300 ? '{"id":"700001"}' : '{}') };
    },
  });
  return { client, requests };
}

async function run(client: ConfluenceClient, context: AttemptContext, maxSendsOverride?: number): Promise<string> {
  const call = { id: callId, connection, operation: AddCommentDefinition.operation, context, ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try { return (await runMutation(client.addComment(), call, { pageId: '600002', body: 'Approved', bodyFormat: 'plainText' })).branch; }
  catch (error) {
    if (error instanceof RetryError) return 'retry';
    throw error;
  }
}

const posts = (requests: ProviderRequest[]) => requests.filter(request => request.method === 'POST').length;

describe('Confluence bounded resend of unconfirmed comments', () => {
  it('reads back and resends an unknown outcome below the cap, then selects uncertain', async () => {
    const { client, requests } = newClient(500);
    const context = new AttemptContext();
    for (; context.attempt <= AddCommentDefinition.maxSends!; context.attempt++) expect(await run(client, context)).toBe('retry');
    expect(posts(requests)).toBe(AddCommentDefinition.maxSends);
    expect(await run(client, context)).toBe('uncertain');
    expect(posts(requests)).toBe(AddCommentDefinition.maxSends);
  });

  it('reads back instead of resending with a send cap of one', async () => {
    const { client, requests } = newClient(500);
    const context = new AttemptContext();
    expect(await run(client, context, 1)).toBe('retry');
    context.attempt = 2;
    expect(await run(client, context, 1)).toBe('uncertain');
    expect(posts(requests)).toBe(1);
  });

  it('records canResend on a definite refusal and never resends it', async () => {
    const { client, requests } = newClient(403);
    const context = new AttemptContext();
    expect(await run(client, context)).toBe('providerRejected');
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(posts(requests)).toBe(1);
  });

  it('selects uncertain when a resend is refused', async () => {
    const context = new AttemptContext();
    expect(await run(newClient(502).client, context)).toBe('retry');
    context.attempt = 2;
    expect(await run(newClient(400).client, context)).toBe('uncertain');
  });

  it('retries an untrusted checkpoint without sending', async () => {
    const { client, requests } = newClient(201);
    expect(await run(client, new AttemptContext(2, { callId: 'another-call', attempt: 1, state: 'claimed' }))).toBe('retry');
    expect(requests).toEqual([]);
  });
});
