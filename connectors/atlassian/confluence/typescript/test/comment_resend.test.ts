// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { MaxErrorBodyBytes, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
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

function newCustomClient(respond: (request: ProviderRequest) => ProviderResponse) {
  const requests: ProviderRequest[] = [];
  const credentials = new StaticCredentialProvider([[connection, { oauthClientId: 'client', oauthClientSecret: new SecretString('secret'), accessToken: new SecretString('token'), refreshToken: new SecretString('') }]]);
  const client = new ConfluenceClient({ endpoint: 'http://127.0.0.1:12345', cloudId: '11223344-a1b2-4b33-8c44-def123456789' }, credentials, {
    send: async (request: ProviderRequest) => { requests.push(request); return respond(request); },
  });
  return { client, requests };
}

function matchingComment(id: string): Record<string, unknown> {
  return { id, pageId: '600002', version: { number: 1, createdAt: new Date().toISOString() }, body: { storage: { representation: 'storage', value: '<p>Approved</p>' } } };
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

  it('records canResend for a 503', async () => {
    const context = new AttemptContext();
    expect(await run(newClient(503).client, context)).toBe('retry');
    expect(context.marker).toMatchObject({ state: 'canResend' });
  });

  it('reads back before sending again after a rate-limited resend', async () => {
    const statuses = [500, 429];
    let reads = 0;
    const { client, requests } = newCustomClient(request => {
      if (request.method === 'POST') return { status: statuses.shift()!, headers: {}, body: Buffer.from('{}') };
      reads++;
      return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({ results: reads === 1 ? [] : [matchingComment('700009')] })) };
    });
    const context = new AttemptContext();
    expect(await run(client, context)).toBe('retry');
    context.attempt = 2;
    expect(await run(client, context)).toBe('retry');
    context.attempt = 3;
    expect(await run(client, context)).toBe('added');
    expect(posts(requests)).toBe(2);
  });

  it('selects uncertain when read-back after a possible send finds no page', async () => {
    const { client, requests } = newCustomClient(() => ({ status: 404, headers: {}, body: Buffer.from('{}') }));
    expect(await run(client, new AttemptContext(2))).toBe('uncertain');
    expect(posts(requests)).toBe(0);
  });

  for (const [isVisible, branch] of [[true, 'added'], [false, 'uncertain']] as const) {
    it(`never resends an accepted comment whose body broke (${branch})`, async () => {
      const { client, requests } = newCustomClient(request => {
        if (request.method === 'POST') throw new ProviderTransportError(false, 201, {});
        return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({ results: isVisible ? [matchingComment('700010')] : [] })) };
      });
      expect(await run(client, new AttemptContext())).toBe(branch);
      expect(posts(requests)).toBe(1);
    });
  }

  it('passes the fence write-window signal to the comment POST', async () => {
    const { client, requests } = newClient(201);
    const context = new AttemptContext();
    expect(await run(client, context)).toBe('added');
    const post = requests.find(request => request.method === 'POST')!;
    expect(post.signal).toBeDefined();
    expect(post.signal).not.toBe(context.cancellationSignal);
  });

  it('retries an untrusted checkpoint without sending', async () => {
    const { client, requests } = newClient(201);
    expect(await run(client, new AttemptContext(2, { callId: 'another-call', attempt: 1, state: 'claimed' }))).toBe('retry');
    expect(requests).toEqual([]);
  });

  for (const [name, respond] of [
    ['a 400 whose body broke', () => { throw new ProviderTransportError(false, 400, {}); }],
    ['a 404 whose body exceeds the error-body limit', () => ({ status: 404, headers: {}, body: Buffer.alloc(MaxErrorBodyBytes + 1) })],
  ] as const) {
    it(`treats ${name} as an unknown outcome`, async () => {
      const { client, requests } = newCustomClient(respond);
      const context = new AttemptContext();
      expect(await run(client, context)).toBe('retry');
      expect(context.marker).toMatchObject({ state: 'claimed' });
      expect(posts(requests)).toBe(1);
    });
  }

  it('never resends a comment accepted before the write deadline aborted its body', async () => {
    const { client, requests } = newCustomClient(request => {
      if (request.method === 'GET') return { status: 200, headers: {}, body: Buffer.from('{"results":[]}') };
      // The SDK keeps the 2xx status when the fence's write-window signal aborts the body.
      throw new ProviderTransportError(false, 201, {});
    });
    expect(await run(client, new AttemptContext())).toBe('uncertain');
    expect(posts(requests)).toBe(1);
  });

  it('retries a transient credential failure and selects uncertain for a terminal one after a possible send', async () => {
    const failing = (error: Error) => new ConfluenceClient({ endpoint: 'http://127.0.0.1:12345', cloudId: '11223344-a1b2-4b33-8c44-def123456789' },
      { resolve: async () => { throw error; }, resolveWithRefresh: async () => { throw error; } } as never,
      { send: async () => { throw new Error('no request is expected'); } });
    const first = new AttemptContext();
    expect(await run(failing(new Error('credential store unavailable')), first)).toBe('retry');
    expect(first.marker).toMatchObject({ state: 'notSent' });
    expect(await run(failing(new ReauthorizationRequiredError()), new AttemptContext())).toBe('defect');
    const earlier = { callId, attempt: 2, state: 'canResend', sendCount: 1 };
    const later = new AttemptContext(3, earlier);
    expect(await run(failing(new Error('credential store unavailable')), later)).toBe('retry');
    expect(later.marker).toBe(earlier);
    expect(await run(failing(new ReauthorizationRequiredError()), new AttemptContext(3, earlier))).toBe('uncertain');
  });

  it.each([
    ['not configured', () => new Error('connection is not configured'), 'retry'],
    ['unlisted type error', () => new TypeError('unlisted'), 'retry'],
    ['broker unavailable', () => new CredentialBrokerUnavailableError(), 'retry'],
    ['not selected', () => new ConnectionNotSelectedError(), 'defect'],
    ['broker rejected', () => new CredentialBrokerRejectedError(), 'defect'],
    ['object not found', () => new ObjectNotFoundError(), 'defect'],
  ])('classifies a credential failure (%s) before any send', async (_name, newError, expected) => {
    const error = newError();
    const client = new ConfluenceClient({ endpoint: 'http://127.0.0.1:12345', cloudId: '11223344-a1b2-4b33-8c44-def123456789' },
      { resolve: async () => { throw error; }, resolveWithRefresh: async () => { throw error; } } as never,
      { send: async () => { throw new Error('no request is expected'); } });
    expect(await run(client, new AttemptContext())).toBe(expected);
  });
});
