// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GmailHTTPClient } from '../src/http.js';
import { GmailClient } from '../src/client.js';
import { GmailProvider } from '../src/provider.js';
import { GmailSendAttemptBudget, GmailSender } from '../src/send.js';
import type { CredentialSource, Credentials } from '../src/models.js';

const connection = { provider: 'google', name: 'gmail-inbox' };
const credentials: Credentials = { accessToken: new SecretString('access-token'), primaryEmail: 'owner@example.com' };
const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false, refresh: async () => { throw new Error('unexpected refresh'); } };

function operationCall(operationId: string, attempt = 1, checkpoint: { value?: unknown } = {}): Call {
  return {
    id: '01234567-89ab-5cde-8f01-23456789abcd', idempotencyKey: '01234567-89ab-5cde-8f01-23456789abcd', connection,
    operation: { connectorId: 'gmail', operationId },
    context: {
      attempt, runId: 'run-1', flowStartedAt: new Date('2026-01-02T00:00:00Z'), cancellationSignal: new AbortController().signal, flowId: 'flow-1',
      hasLastHeartbeatValue: () => checkpoint.value !== undefined,
      getLastHeartbeatValue: () => checkpoint.value,
      recordHeartbeat: async (value: unknown) => { checkpoint.value = value; },
    },
  } as Call;
}

function response(status: number, body: unknown, headers: Record<string, string> = {}): ProviderResponse {
  return { status, headers, body: Buffer.from(JSON.stringify(body)) };
}

function sender(transport: (request: ProviderRequest) => Promise<ProviderResponse>, maxMessageBytes?: number): GmailSender {
  const http = new GmailHTTPClient({ endpoint: 'http://127.0.0.1:8080', ...(maxMessageBytes ? { maxMessageBytes } : {}) }, new StaticCredentialProvider([[connection, credentials]]), driver, transport);
  return new GmailSender(http, new GmailProvider(http));
}

const sendInput = { to: ['ada@example.com'], subject: 'Progress', textBody: 'Update' };
const replyInput = { messageId: 'source-1', textBody: 'Answer' };

describe('Gmail send and reply classification', () => {
  it('exposes both Mutations with the stable Call ID and Dex attempt deadline', async () => {
    let posts = 0;
    const client = new GmailClient({ endpoint: 'http://127.0.0.1:8080' }, new StaticCredentialProvider([[connection, credentials]]), {
      refreshDriver: driver, transport: async () => { posts++; return response(200, {}); },
    });
    const send = client.sendMessage();
    const reply = client.replyToMessage();
    expect(send.definition().operation.operationId).toBe('sendMessage');
    expect(reply.definition().operation.operationId).toBe('replyToMessage');
    expect(send.idempotencyKey('stable-call-id', sendInput)).toBe('stable-call-id');
    expect(reply.idempotencyKey('stable-call-id', replyInput)).toBe('stable-call-id');
    const original = operationCall('sendMessage');
    const expiredCall = { ...original, context: { ...original.context, deadline: new Date(Date.now() - 1) } } as Call;
    expect(await send.invoke(expiredCall, sendInput)).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    expect(posts).toBe(0);
  });

  it('reserves send, read-back, and Result time inside the Dex attempt deadline', () => {
    const started = new Date();
    const budget = new GmailSendAttemptBudget(new AbortController().signal, new Date(started.getTime() + 6000), started);
    expect(budget.sendDeadline - started.getTime()).toBe(3500);
    expect(budget.attemptDeadline - started.getTime()).toBe(5000);
    expect(budget.minimumSendMs).toBe(600);
  });

  it('posts one raw message with stable Message-ID and returns its receipt', async () => {
    let postCount = 0;
    const operation = sender(async request => {
      expect(request.method).toBe('POST');
      postCount++;
      const payload = JSON.parse(Buffer.from(request.body ?? []).toString()) as { raw: string };
      const raw = Buffer.from(payload.raw, 'base64url').toString();
      expect(raw).toContain('From: owner@example.com\r\n');
      expect(raw).toContain('To: <ada@example.com>\r\n');
      expect(raw).toContain('Message-ID: <');
      expect(raw).not.toContain('access-token');
      return response(200, { id: 'sent-1', threadId: 'thread-1' }, { 'x-goog-request-id': 'request-1' });
    });
    const result = await operation.sendMessage(operationCall('sendMessage'), sendInput, new Date(Date.now() + 30_000));
    expect(postCount).toBe(1);
    expect(result).toMatchObject({ kind: 'branch', branch: 'sent', value: { sender: 'owner@example.com', recipients: ['<ada@example.com>'], messageId: 'sent-1', threadId: 'thread-1' }, receipt: { providerRequestId: 'request-1', providerObjectId: 'sent-1' } });
  });

  it('reads a missing thread ID from Gmail minimal metadata', async () => {
    const operation = sender(async request => request.method === 'POST'
      ? response(200, { id: 'sent-1' })
      : response(200, { id: 'sent-1', threadId: 'thread-1' }));
    const result = await operation.sendMessage(operationCall('sendMessage'), sendInput, new Date(Date.now() + 30_000));
    expect(result).toMatchObject({ branch: 'sent', value: { messageId: 'sent-1', threadId: 'thread-1' } });
  });

  it('selects defect before a POST for invalid input and oversize messages', async () => {
    let calls = 0;
    const operation = sender(async () => { calls++; return response(200, {}); });
    expect(await operation.sendMessage(operationCall('sendMessage'), { ...sendInput, to: [] }, new Date(Date.now() + 30_000))).toMatchObject({ branch: 'defect', failure: { kind: 'VALIDATION' } });
    expect(await operation.sendMessage(operationCall('sendMessage'), { ...sendInput, to: ['bad-address'] }, new Date(Date.now() + 30_000))).toMatchObject({ branch: 'defect', failure: { message: 'recipient email is invalid' } });
    expect(await operation.sendMessage(operationCall('sendMessage'), { ...sendInput, to: ['ada@example.com', 'ADA@example.com'] }, new Date(Date.now() + 30_000))).toMatchObject({ branch: 'defect', failure: { message: 'recipient email is duplicated' } });
    expect(await operation.replyToMessage(operationCall('replyToMessage'), { ...replyInput, textBody: '' }, new Date(Date.now() + 30_000))).toMatchObject({ branch: 'defect', failure: { kind: 'VALIDATION' } });
    expect(calls).toBe(0);
    const limited = sender(async () => { calls++; return response(200, {}); }, 1);
    expect(await limited.sendMessage(operationCall('sendMessage'), sendInput, new Date(Date.now() + 30_000))).toMatchObject({ branch: 'defect', failure: { kind: 'VALIDATION' } });
    expect(calls).toBe(0);
  });

  it('retries a 429 with Retry-After and classifies a conclusive 403', async () => {
    const rateLimited = sender(async () => response(429, {}, { 'retry-after': '2' }));
    expect(await rateLimited.sendMessage(operationCall('sendMessage'), sendInput, new Date(Date.now() + 30_000))).toMatchObject({ kind: 'retry', retryAfterMs: 2000, failure: { kind: 'RATE_LIMIT' } });
    const rejected = sender(async () => response(403, {}));
    expect(await rejected.sendMessage(operationCall('sendMessage'), sendInput, new Date(Date.now() + 30_000))).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHORIZATION' } });
  });

  it('returns an authentication defect before a send when connection credentials are unavailable', async () => {
    let posts = 0;
    const http = new GmailHTTPClient({ endpoint: 'http://127.0.0.1:8080' }, { resolve: async () => { throw new Error('private token is unavailable'); }, resolveWithRefresh: async () => { throw new Error('private token is unavailable'); } }, driver, async () => { posts++; return response(200, {}); });
    const operation = new GmailSender(http, new GmailProvider(http));
    expect(await operation.sendMessage(operationCall('sendMessage'), sendInput, new Date(Date.now() + 30_000))).toMatchObject({ branch: 'defect', failure: { kind: 'AUTHENTICATION', message: 'connection credentials are unavailable' } });
    expect(await operation.replyToMessage(operationCall('replyToMessage'), replyInput, new Date(Date.now() + 30_000))).toMatchObject({ branch: 'defect', failure: { kind: 'AUTHENTICATION' } });
    expect(posts).toBe(0);
  });

  it('refreshes a rejected access token once before classifying a send', async () => {
    let requests = 0;
    let refreshes = 0;
    const provider: CredentialSource & { resolveAfterRejection: (activeCall: Call, refreshDriver: CredentialRefreshDriver<Credentials>) => Promise<Credentials> } = {
      resolve: async () => credentials,
      resolveWithRefresh: async () => credentials,
      resolveAfterRejection: async () => { refreshes++; return { ...credentials, accessToken: new SecretString('replacement-token') }; },
    };
    const http = new GmailHTTPClient({ endpoint: 'http://127.0.0.1:8080' }, provider, driver, async request => {
      requests++;
      expect(request.headers?.Authorization).toBe(requests === 1 ? 'Bearer access-token' : 'Bearer replacement-token');
      return requests === 1 ? response(401, {}) : response(200, { id: 'sent-1', threadId: 'thread-1' });
    });
    const operation = new GmailSender(http, new GmailProvider(http));
    expect(await operation.sendMessage(operationCall('sendMessage'), sendInput, new Date(Date.now() + 30_000))).toMatchObject({ branch: 'sent', value: { messageId: 'sent-1' } });
    expect(requests).toBe(2);
    expect(refreshes).toBe(1);
  });

  it('selects authentication rejection after one unsuccessful forced refresh', async () => {
    let requests = 0;
    let refreshes = 0;
    const provider: CredentialSource & { resolveAfterRejection: (activeCall: Call, refreshDriver: CredentialRefreshDriver<Credentials>) => Promise<Credentials> } = {
      resolve: async () => credentials,
      resolveWithRefresh: async () => credentials,
      resolveAfterRejection: async () => { refreshes++; return { ...credentials, accessToken: new SecretString('replacement-token') }; },
    };
    const http = new GmailHTTPClient({ endpoint: 'http://127.0.0.1:8080' }, provider, driver, async () => { requests++; return response(401, {}); });
    const operation = new GmailSender(http, new GmailProvider(http));
    expect(await operation.sendMessage(operationCall('sendMessage'), sendInput, new Date(Date.now() + 30_000))).toMatchObject({ branch: 'providerRejected', failure: { kind: 'AUTHENTICATION' } });
    expect(requests).toBe(2);
    expect(refreshes).toBe(1);
  });

  it('retries provably undispatched transport failure without Sent Mail lookup', async () => {
    let calls = 0;
    const operation = sender(async () => { calls++; throw new ProviderTransportError(true); });
    const result = await operation.sendMessage(operationCall('sendMessage'), sendInput, new Date(Date.now() + 30_000));
    expect(result).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT' } });
    expect(calls).toBe(1);
  });

  it('returns Retry before POST when the Execute attempt has too little time', async () => {
    let posts = 0;
    const operation = sender(async () => { posts++; return response(200, {}); });
    const result = await operation.sendMessage(operationCall('sendMessage'), sendInput, new Date(Date.now() - 1));
    expect(result).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    expect(posts).toBe(0);
  });

  it('checks Sent Mail after ambiguous transport and confirms a found send', async () => {
    let postCount = 0;
    let searchCount = 0;
    const operation = sender(async request => {
      if (request.method === 'POST') { postCount++; throw new ProviderTransportError(false); }
      searchCount++;
      expect(request.url).toContain('rfc822msgid%3A');
      return response(200, { messages: [{ id: 'found-1', threadId: 'thread-1' }] });
    });
    const result = await operation.sendMessage(operationCall('sendMessage'), sendInput, new Date(Date.now() + 30_000));
    expect(result).toMatchObject({ branch: 'sent', value: { messageId: 'found-1' } });
    expect(postCount).toBe(1);
    expect(searchCount).toBe(1);
  });

  it('retries an ambiguous send after all four Sent Mail searches miss', async () => {
    let searches = 0;
    const operation = sender(async request => {
      if (request.method === 'POST') throw new ProviderTransportError(false);
      searches++;
      return response(200, { messages: [] });
    });
    const result = await operation.sendMessage(operationCall('sendMessage'), sendInput, new Date(Date.now() + 30_000));
    expect(result).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT', message: 'Gmail send outcome is unknown; the sent-mail search found no message' } });
    expect(searches).toBe(4);
  });

  it('keeps an ambiguous send claimed and searches without a second POST on retry', async () => {
    const checkpoint: { value?: unknown } = {};
    let posts = 0;
    let searches = 0;
    const operation = sender(async request => {
      if (request.method === 'POST') { posts++; throw new ProviderTransportError(false); }
      searches++;
      return response(200, { messages: [] });
    });
    expect((await operation.sendMessage(operationCall('sendMessage', 1, checkpoint), sendInput, new Date(Date.now() + 30_000))).kind).toBe('retry');
    expect(checkpoint.value).toMatchObject({ state: 'claimed', attempt: 1 });
    expect((await operation.sendMessage(operationCall('sendMessage', 2, checkpoint), sendInput, new Date(Date.now() + 30_000))).kind).toBe('retry');
    expect(posts).toBe(1);
    expect(searches).toBe(5);
  });

  it('records conclusive rate-limit refusal so the next attempt may send', async () => {
    const checkpoint: { value?: unknown } = {};
    let posts = 0;
    const operation = sender(async request => {
      if (request.method === 'GET') return response(200, { messages: [] });
      posts++;
      return posts === 1 ? response(429, {}) : response(200, { id: 'sent-after-limit', threadId: 'thread-1' });
    });
    expect((await operation.sendMessage(operationCall('sendMessage', 1, checkpoint), sendInput, new Date(Date.now() + 30_000))).kind).toBe('retry');
    expect(checkpoint.value).toMatchObject({ state: 'canResend', attempt: 1 });
    expect(await operation.sendMessage(operationCall('sendMessage', 2, checkpoint), sendInput, new Date(Date.now() + 30_000)))
      .toMatchObject({ branch: 'sent', value: { messageId: 'sent-after-limit' } });
    expect(posts).toBe(2);
    expect(checkpoint.value).toMatchObject({ state: 'claimed', providerId: 'sent-after-limit', attempt: 2 });
  });

  it('ignores an ID in a 503 body and resends after Sent Mail misses', async () => {
    const checkpoint: { value?: unknown } = {};
    let posts = 0;
    let searches = 0;
    const operation = sender(async request => {
      if (request.method === 'GET') { searches++; return response(200, { messages: [] }); }
      posts++;
      return posts === 1 ? response(503, { id: 'not-a-message' }) : response(200, { id: 'sent-after-503', threadId: 'thread-1' });
    });
    expect(await operation.sendMessage(operationCall('sendMessage', 1, checkpoint), sendInput, new Date(Date.now() + 30_000)))
      .toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    expect(checkpoint.value).toMatchObject({ state: 'canResend', attempt: 1 });
    expect(checkpoint.value).not.toHaveProperty('providerId');
    expect(await operation.sendMessage(operationCall('sendMessage', 2, checkpoint), sendInput, new Date(Date.now() + 30_000)))
      .toMatchObject({ branch: 'sent', value: { messageId: 'sent-after-503' } });
    expect(posts).toBe(2);
    expect(searches).toBe(5);
  });

  it('never checkpoints an ID from a 500 or 400 error body', async () => {
    for (const status of [500, 400]) {
      const checkpoint: { value?: unknown } = {};
      let posts = 0;
      let searches = 0;
      const operation = sender(async request => {
        if (request.method === 'GET') { searches++; return response(200, { messages: [] }); }
        posts++;
        return response(status, { id: 'not-a-message' });
      });
      const first = await operation.sendMessage(operationCall('sendMessage', 1, checkpoint), sendInput, new Date(Date.now() + 30_000));
      expect(first).toMatchObject(status === 500 ? { kind: 'retry' } : { branch: 'providerRejected' });
      expect(checkpoint.value).not.toHaveProperty('providerId');
      if (status === 500) {
        expect(checkpoint.value).toMatchObject({ state: 'claimed', attempt: 1 });
        expect(await operation.sendMessage(operationCall('sendMessage', 2, checkpoint), sendInput, new Date(Date.now() + 30_000)))
          .toMatchObject({ kind: 'retry' });
        expect(posts).toBe(1);
        expect(searches).toBe(5);
      }
    }
  });

  it('uses a checkpointed provider ID on redispatch without another POST or Sent Mail search', async () => {
    const checkpoint: { value?: unknown } = {};
    let posts = 0;
    let searches = 0;
    const operation = sender(async request => {
      if (request.method === 'POST') { posts++; return response(200, { id: 'checkpointed-send', threadId: 'thread-1' }); }
      if (request.url.includes('/users/me/messages?')) searches++;
      return response(200, { id: 'checkpointed-send', threadId: 'thread-1' });
    });
    expect((await operation.sendMessage(operationCall('sendMessage', 1, checkpoint), sendInput, new Date(Date.now() + 30_000))).kind).toBe('branch');
    expect(checkpoint.value).toMatchObject({ state: 'claimed', providerId: 'checkpointed-send' });
    expect(await operation.sendMessage(operationCall('sendMessage', 2, checkpoint), sendInput, new Date(Date.now() + 30_000)))
      .toMatchObject({ branch: 'sent', value: { messageId: 'checkpointed-send', threadId: 'thread-1' } });
    expect(posts).toBe(1);
    expect(searches).toBe(0);
  });

  it('does not POST when Dex cannot record the send claim', async () => {
    let posts = 0;
    const operation = sender(async () => { posts++; return response(200, { id: 'unexpected' }); });
    const call = operationCall('sendMessage');
    const failedCheckpointCall = { ...call, context: { ...call.context, recordHeartbeat: async () => { throw new Error('checkpoint unavailable'); } } } as Call;
    expect(await operation.sendMessage(failedCheckpointCall, sendInput, new Date(Date.now() + 30_000)))
      .toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    expect(posts).toBe(0);
  });

  it('searches before a numbered retry and never posts when already sent', async () => {
    let postCount = 0;
    const operation = sender(async request => {
      if (request.method === 'POST') postCount++;
      return response(200, { messages: [{ id: 'previous-1', threadId: 'thread-1' }] });
    });
    const result = await operation.sendMessage(operationCall('sendMessage', 2), sendInput, new Date(Date.now() + 30_000));
    expect(result).toMatchObject({ branch: 'sent', value: { messageId: 'previous-1' } });
    expect(postCount).toBe(0);
  });

  it('accepts a malformed 2xx send response after bounded read-back', async () => {
    let searches = 0;
    const operation = sender(async request => {
      if (request.method === 'POST') return response(200, { unusable: true });
      searches++;
      return response(200, { messages: [] });
    });
    const result = await operation.sendMessage(operationCall('sendMessage'), sendInput, new Date(Date.now() + 30_000));
    expect(result).toMatchObject({ branch: 'sent', value: { messageId: '', threadId: '' } });
    expect(searches).toBe(4);
  });

  it('accepts an oversized 2xx send response after bounded read-back', async () => {
    let searches = 0;
    const operation = sender(async request => {
      if (request.method === 'POST') throw new BodyTooLargeError(200, {});
      searches++;
      return response(200, { messages: [] });
    });
    const result = await operation.sendMessage(operationCall('sendMessage'), sendInput, new Date(Date.now() + 30_000));
    expect(result).toMatchObject({ branch: 'sent', value: { messageId: '' } });
    expect(searches).toBe(4);
  });

  it('retries an oversized 503 after read-back and preserves Retry-After', async () => {
    const operation = sender(async request => {
      if (request.method === 'POST') throw new BodyTooLargeError(503, { 'retry-after': '4' });
      return response(200, { messages: [] });
    });
    const result = await operation.sendMessage(operationCall('sendMessage'), sendInput, new Date(Date.now() + 30_000));
    expect(result).toMatchObject({ kind: 'retry', retryAfterMs: 4000, failure: { kind: 'AVAILABILITY' } });
  });

  it('reads source metadata and posts a reply in its thread', async () => {
    let posts = 0;
    const operation = sender(async request => {
      if (request.method === 'GET') {
        expect(request.url).toContain('format=metadata');
        return response(200, { id: 'source-1', threadId: 'thread-1', internalDate: '1767312000000', payload: { headers: [
          { name: 'Message-ID', value: '<source@example.com>' }, { name: 'From', value: 'Ada <ada@example.com>' },
          { name: 'Reply-To', value: 'reply@example.com' }, { name: 'Subject', value: 'Question' },
        ] } });
      }
      posts++;
      const payload = JSON.parse(Buffer.from(request.body ?? []).toString()) as { raw: string; threadId: string };
      expect(payload.threadId).toBe('thread-1');
      expect(Buffer.from(payload.raw, 'base64url').toString()).toContain('In-Reply-To: <source@example.com>\r\n');
      return response(200, { id: 'reply-1', threadId: 'thread-1' });
    });
    const result = await operation.replyToMessage(operationCall('replyToMessage'), replyInput, new Date(Date.now() + 30_000));
    expect(result).toMatchObject({ branch: 'sent', value: { messageId: 'reply-1', threadId: 'thread-1', recipients: ['<reply@example.com>'] } });
    expect(posts).toBe(1);
  });

  it('retries a reply POST refusal with Retry-After after reading the source', async () => {
    const operation = sender(async request => request.url.includes('/source-1?')
      ? response(200, { id: 'source-1', threadId: 'thread-1', internalDate: '1767312000000', payload: { headers: [
        { name: 'Message-ID', value: '<source@example.com>' }, { name: 'From', value: 'ada@example.com' },
      ] } })
      : response(429, {}, { 'retry-after': '3' }));
    expect(await operation.replyToMessage(operationCall('replyToMessage'), replyInput, new Date(Date.now() + 30_000))).toMatchObject({
      kind: 'retry', retryAfterMs: 3000, failure: { kind: 'RATE_LIMIT', operation: 'replyToMessage' },
    });
  });

  it('records canResend after a 503 reply miss and posts again on the next attempt', async () => {
    const checkpoint: { value?: unknown } = {};
    let posts = 0;
    let searches = 0;
    const operation = sender(async request => {
      if (request.url.includes('/source-1?')) return response(200, { id: 'source-1', threadId: 'thread-1', internalDate: '1767312000000', payload: { headers: [
        { name: 'Message-ID', value: '<source@example.com>' }, { name: 'From', value: 'ada@example.com' },
      ] } });
      if (request.method === 'GET') { searches++; return response(200, { messages: [] }); }
      posts++;
      return posts === 1 ? response(503, { id: 'not-a-reply' }) : response(200, { id: 'reply-after-503', threadId: 'thread-1' });
    });
    expect(await operation.replyToMessage(operationCall('replyToMessage', 1, checkpoint), replyInput, new Date(Date.now() + 30_000)))
      .toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    expect(checkpoint.value).toMatchObject({ state: 'canResend', attempt: 1 });
    expect(checkpoint.value).not.toHaveProperty('providerId');
    expect(await operation.replyToMessage(operationCall('replyToMessage', 2, checkpoint), replyInput, new Date(Date.now() + 30_000)))
      .toMatchObject({ branch: 'sent', value: { messageId: 'reply-after-503' } });
    expect(posts).toBe(2);
    expect(searches).toBe(5);
  });

  it('accepts an oversized 2xx reply response without resending when Sent Mail has no match', async () => {
    let posts = 0;
    const operation = sender(async request => {
      if (request.url.includes('/source-1?')) return response(200, { id: 'source-1', threadId: 'thread-1', internalDate: '1767312000000', payload: { headers: [
        { name: 'Message-ID', value: '<source@example.com>' }, { name: 'From', value: 'ada@example.com' },
      ] } });
      if (request.method === 'POST') { posts++; throw new BodyTooLargeError(200, {}); }
      return response(200, { messages: [] });
    });
    expect(await operation.replyToMessage(operationCall('replyToMessage'), replyInput, new Date(Date.now() + 30_000))).toMatchObject({
      branch: 'sent', value: { messageId: '', threadId: 'thread-1', recipients: ['<ada@example.com>'] },
    });
    expect(posts).toBe(1);
  });

  it('confirms a previous reply before reading its source or posting again', async () => {
    let searches = 0;
    const operation = sender(async request => {
      expect(request.url).toContain('/users/me/messages?');
      searches++;
      return response(200, { messages: [{ id: 'earlier-reply', threadId: 'thread-1' }] });
    });
    const result = await operation.replyToMessage(operationCall('replyToMessage', 2), replyInput, new Date(Date.now() + 30_000));
    expect(result).toMatchObject({ branch: 'sent', value: { recipients: [], messageId: 'earlier-reply', threadId: 'thread-1' } });
    expect(searches).toBe(1);
  });

  it('read-backs an ambiguous reply on retry without repeating source lookup or POST', async () => {
    const checkpoint: { value?: unknown } = {};
    let sourceReads = 0;
    let posts = 0;
    let searches = 0;
    const operation = sender(async request => {
      if (request.url.includes('/source-1?')) {
        sourceReads++;
        return response(200, { id: 'source-1', threadId: 'thread-1', internalDate: '1767312000000', payload: { headers: [
          { name: 'Message-ID', value: '<source@example.com>' }, { name: 'From', value: 'ada@example.com' },
        ] } });
      }
      if (request.method === 'POST') { posts++; throw new ProviderTransportError(false); }
      searches++;
      return response(200, { messages: searches >= 5 ? [{ id: 'confirmed-reply', threadId: 'thread-1' }] : [] });
    });
    expect((await operation.replyToMessage(operationCall('replyToMessage', 1, checkpoint), replyInput, new Date(Date.now() + 30_000))).kind).toBe('retry');
    expect(checkpoint.value).toMatchObject({ state: 'claimed', attempt: 1 });
    expect(await operation.replyToMessage(operationCall('replyToMessage', 2, checkpoint), replyInput, new Date(Date.now() + 30_000)))
      .toMatchObject({ branch: 'sent', value: { messageId: 'confirmed-reply', threadId: 'thread-1' } });
    expect(sourceReads).toBe(1);
    expect(posts).toBe(1);
    expect(searches).toBe(5);
  });

  it('classifies missing, malformed, and oversized source responses before sending', async () => {
    const missing = sender(async () => response(404, {}));
    expect(await missing.replyToMessage(operationCall('replyToMessage'), replyInput, new Date(Date.now() + 30_000))).toMatchObject({ branch: 'providerRejected', failure: { kind: 'NOT_FOUND' } });
    const malformed = sender(async () => response(200, { id: 'source-1' }));
    expect(await malformed.replyToMessage(operationCall('replyToMessage'), replyInput, new Date(Date.now() + 30_000))).toMatchObject({ branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
    const oversized = sender(async () => { throw new BodyTooLargeError(200, {}); });
    expect(await oversized.replyToMessage(operationCall('replyToMessage'), replyInput, new Date(Date.now() + 30_000))).toMatchObject({ branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
  });

  it('retries transient source reads with Retry-After', async () => {
    const operation = sender(async () => response(429, {}, { 'retry-after': '5' }));
    const result = await operation.replyToMessage(operationCall('replyToMessage'), replyInput, new Date(Date.now() + 30_000));
    expect(result).toMatchObject({ kind: 'retry', retryAfterMs: 5000, failure: { kind: 'AVAILABILITY' } });
  });
});
