// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { LinearClient } from '../src/client.js';

const connection = { provider: 'linear', name: 'linear-test' };
const credentials = new StaticCredentialProvider([[connection, { authMethodId: 'personal-api-key', apiKey: new SecretString('linear-test-key') }]]);
const call = { id: 'ce503c25-24ca-5820-aaf4-2c4204858c7f', idempotencyKey: 'ce503c25-24ca-5820-aaf4-2c4204858c7f', connection, operation: { connectorId: 'linear', operationId: 'createIssue' }, context: { cancellationSignal: new AbortController().signal } } as Call;

describe('Linear input and configuration validation', () => {
  it('rejects a noncalendar due date before sending', async () => {
    let sent = false;
    const client = new LinearClient({}, credentials, { send: async () => { sent = true; throw new Error('unexpected request'); } });
    const attempt = await client.createIssue().invoke(call, { teamId: '2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4', title: 'Fix calendar', dueDate: '2026-02-29' });
    expect(attempt).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'VALIDATION' } });
    expect(sent).toBe(false);
  });

  it('validates every declared connection limit', () => {
    expect(() => new LinearClient({ maxResponseBytes: 0 }, credentials)).toThrow();
    expect(() => new LinearClient({ webhookMaxBodyBytes: 0 }, credentials)).toThrow();
    expect(() => new LinearClient({ webhookSignatureToleranceMs: 999 }, credentials)).toThrow();
  });

  it('classifies a local transport refusal as Retry', async () => {
    const client = new LinearClient({}, credentials, { send: async () => { throw new Error('connection refused'); } });
    const attempt = await client.getIssue().invoke(call, { issueId: 'ENG-7' });
    expect(attempt).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT' } });
  });

  it('keeps a large rate limit response retryable', async () => {
    const client = new LinearClient({ maxResponseBytes: 16 }, credentials, { send: async () => { throw new BodyTooLargeError(429, { 'retry-after': '2' }); } });
    const attempt = await client.getIssue().invoke(call, { issueId: 'ENG-7' });
    expect(attempt).toMatchObject({ kind: 'retry', failure: { kind: 'RATE_LIMIT' }, retryAfterMs: 2_000 });
  });

  it('keeps a rejected credential terminal', async () => {
    const client = new LinearClient({}, credentials, { send: async () => ({ status: 401, headers: {}, body: Buffer.from('{}') }) });
    const attempt = await client.getIssue().invoke(call, { issueId: 'ENG-7' });
    expect(attempt).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHENTICATION' } });
  });

  it('resends one 401 after host refresh of an OAuth token', async () => {
    const sentTokens: string[] = [];
    const renewableCredentials = {
      resolve: async () => ({ authMethodId: 'linear-oauth', accessToken: new SecretString('old-token') }),
      resolveWithRefresh: async () => ({ authMethodId: 'linear-oauth', accessToken: new SecretString('old-token') }),
      resolveAfterRejection: async () => ({ authMethodId: 'linear-oauth', accessToken: new SecretString('new-token') }),
    };
    const client = new LinearClient({}, renewableCredentials, { send: async request => {
      sentTokens.push(request.headers?.Authorization ?? '');
      return sentTokens.length === 1 ? { status: 401, headers: {}, body: Buffer.from('{}') } :
        { status: 200, headers: {}, body: Buffer.from('{"data":{"issues":{"nodes":[]}}}') };
    } });
    const attempt = await client.getIssue().invoke(call, { issueId: 'ENG-7' });
    expect(attempt).toMatchObject({ kind: 'branch', branch: 'notFound' });
    expect(sentTokens).toEqual(['Bearer old-token', 'Bearer new-token']);
  });
});
