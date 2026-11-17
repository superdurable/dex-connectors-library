// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { NotionClient } from '../src/client.js';
import { CreatePageDefinition } from '../src/generated/definitions.js';
import type { CreatePageOutput } from '../src/generated/models.js';

const callId = '6eea9f59-2558-5319-bc0f-28be0a0e2fd3';
const dataSourceId = '2a4d6f80-1b3c-4d5e-8f70-112233445566';
const pageId = '1f3c9a2e-5b7d-4e8a-9c01-23456789abcd';
const maxSends = CreatePageDefinition.maxSends!;
const page = { object: 'page', id: pageId, parent: { type: 'data_source_id', data_source_id: dataSourceId }, properties: { Name: { id: 'title', type: 'title', title: [] } } };

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'create-page-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 120_000);
  readonly cancellationSignal = new AbortController().signal;
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

function newClient(answer: (request: ProviderRequest) => { status: number; body: unknown }) {
  const requests: ProviderRequest[] = [];
  const client = new NotionClient({}, { resolve: async () => ({ apiToken: new SecretString('ntn_test_token_value') }) }, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      const { status, body } = request.method === 'GET' ? { status: 200, body: page } : answer(request);
      return { status, headers: {}, body: Buffer.from(JSON.stringify(body)) };
    },
  });
  return { client, requests, posts: () => requests.filter(request => request.method === 'POST').length };
}

async function createPage(client: NotionClient, context: AttemptContext): Promise<MutationResult<CreatePageOutput> | RetryError> {
  const call = { id: callId, connection: { provider: 'notion', name: 'notion-test' }, operation: CreatePageDefinition.operation, context } as unknown as Call;
  try { return await runMutation(client.createPage(), call, { dataSourceId, properties: { Name: { type: 'title', text: 'Ada' } } }); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

describe('Notion createPage bounded resend', () => {
  it.each([500, 409, 529])('resends an unknown HTTP %i until the send cap, then selects uncertain', async status => {
    const { client, posts } = newClient(() => ({ status, body: { object: 'error', code: 'unknown' } }));
    const context = new AttemptContext();
    for (let attempt = 1; attempt < maxSends; attempt++) {
      context.attempt = attempt;
      expect(await createPage(client, context)).toBeInstanceOf(RetryError);
      expect(context.marker).toMatchObject({ state: 'claimed', sendCount: attempt });
    }
    context.attempt = maxSends;
    expect(await createPage(client, context)).toMatchObject({ branch: 'uncertain' });
    context.attempt++;
    expect(await createPage(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(posts()).toBe(maxSends);
  });

  it('records canResend before selecting providerRejected for a definite first-send refusal', async () => {
    const { client, posts } = newClient(() => ({ status: 400, body: { object: 'error', code: 'validation_error' } }));
    const context = new AttemptContext();
    expect(await createPage(client, context)).toMatchObject({ branch: 'providerRejected' });
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(posts()).toBe(1);
  });

  it('selects uncertain for a refusal after a possible send', async () => {
    let index = 0;
    const { client, posts } = newClient(() => ({ status: index++ === 0 ? 502 : 400, body: { object: 'error' } }));
    const context = new AttemptContext();
    expect(await createPage(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await createPage(client, context)).toMatchObject({ branch: 'uncertain' });
    expect(posts()).toBe(2);
  });

  it('reads back a saved page ID instead of resending', async () => {
    const { client, requests } = newClient(() => ({ status: 500, body: {} }));
    const context = new AttemptContext();
    context.attempt = 2;
    context.marker = { callId, attempt: 1, state: 'claimed', providerId: pageId, sendCount: 1 };
    expect(await createPage(client, context)).toMatchObject({ branch: 'created', value: { pageId } });
    expect(requests.map(request => request.method)).toEqual(['GET']);
  });

  it('returns Retry without sending for an untrusted checkpoint', async () => {
    const { client, requests } = newClient(() => ({ status: 200, body: page }));
    const context = new AttemptContext();
    context.attempt = 2;
    context.marker = { callId: 'foreign', attempt: 1, state: 'claimed' };
    expect(await createPage(client, context)).toBeInstanceOf(RetryError);
    expect(requests).toHaveLength(0);
  });
});
