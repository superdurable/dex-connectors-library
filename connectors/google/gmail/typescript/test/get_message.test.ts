// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { SecretString, StaticCredentialProvider, type Call, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GmailClient, type CredentialSource, type Credentials } from '../src/index.js';

const connection = { provider: 'google', name: 'gmail-inbox' };
const credentials: Credentials = { accessToken: new SecretString('access-token'), primaryEmail: 'owner@example.com' };
const call = {
  id: '01234567-89ab-5cde-8f01-23456789abcd', connection, operation: { connectorId: 'gmail', operationId: 'getMessage' },
  context: { cancellationSignal: new AbortController().signal, flowId: 'flow-1' },
} as Call;
const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false, refresh: async () => { throw new Error('unexpected refresh'); } };

function response(status: number, body: unknown, headers: Record<string, string> = {}): ProviderResponse {
  return { status, headers, body: Buffer.from(JSON.stringify(body)) };
}

function makeClient(transport: (request: ProviderRequest) => Promise<ProviderResponse>, credentialProvider: CredentialSource = new StaticCredentialProvider([[connection, credentials]])) {
  return new GmailClient({ endpoint: 'http://127.0.0.1:8080' }, credentialProvider, { transport, refreshDriver: driver, now: () => new Date('2026-01-02T00:00:00Z') });
}

describe('getMessage operation', () => {
  it('reads and decodes a full message with bounded authenticated request', async () => {
    let request: ProviderRequest | undefined;
    const client = makeClient(async current => {
      request = current;
      return response(200, { id: 'message-1', threadId: 'thread-1', internalDate: '1767312000000', payload: { mimeType: 'text/plain', body: { data: Buffer.from('Hello').toString('base64url') }, headers: [{ name: 'From', value: 'Ada <ada@example.com>' }, { name: 'Subject', value: 'Question' }] } }, { 'x-goog-request-id': 'request-1' });
    });
    const result = await client.getMessage().invoke(call, { messageId: 'message-1' });
    expect(result).toMatchObject({ kind: 'branch', branch: 'read', value: { messageId: 'message-1', textBody: 'Hello' }, receipt: { providerRequestId: 'request-1', providerObjectId: 'message-1' } });
    expect(request).toMatchObject({ method: 'GET', addressPolicy: 'privateNetworkHTTP', maxResponseBytes: 1_048_576, headers: { Authorization: 'Bearer access-token' } });
    expect(request?.url).toContain('/users/me/messages/message-1?format=full');
  });

  it('selects every non-happy branch without sending invalid local input', async () => {
    let calls = 0;
    const client = makeClient(async () => { calls++; return response(404, {}); });
    expect(await client.getMessage().invoke(call, { messageId: '' })).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'VALIDATION' } });
    expect(calls).toBe(0);
    expect(await client.getMessage().invoke(call, { messageId: 'missing' })).toMatchObject({ branch: 'notFound' });
    const rejected = makeClient(async () => response(403, {}));
    expect(await rejected.getMessage().invoke(call, { messageId: 'one' })).toMatchObject({ branch: 'providerRejected', failure: { kind: 'AUTHORIZATION' } });
    const malformed = makeClient(async () => response(200, { id: 'one' }));
    expect(await malformed.getMessage().invoke(call, { messageId: 'one' })).toMatchObject({ branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
    const absentCredentials = makeClient(async () => response(200, {}), { resolve: async () => { throw new Error('secret must not leak'); }, resolveWithRefresh: async () => { throw new Error('secret must not leak'); } });
    expect(await absentCredentials.getMessage().invoke(call, { messageId: 'one' })).toMatchObject({ branch: 'defect', failure: { kind: 'AUTHENTICATION', message: 'connection credentials are unavailable' } });
  });

  it('retries 429 and 503 with integer Retry-After and rejects malformed delay', async () => {
    const rateLimited = makeClient(async () => response(429, {}, { 'retry-after': '3' }));
    expect(await rateLimited.getMessage().invoke(call, { messageId: 'one' })).toMatchObject({ kind: 'retry', retryAfterMs: 3000, failure: { kind: 'AVAILABILITY' } });
    const unavailable = makeClient(async () => response(503, {}, { 'retry-after': 'garbage' }));
    expect(await unavailable.getMessage().invoke(call, { messageId: 'one' })).toMatchObject({ kind: 'retry', failure: { kind: 'PROTOCOL' } });
  });

  it('distinguishes oversized 2xx from oversized rate-limit responses', async () => {
    const invalid = makeClient(async () => { throw new BodyTooLargeError(200, {}); });
    expect(await invalid.getMessage().invoke(call, { messageId: 'one' })).toMatchObject({ branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
    const retry = makeClient(async () => { throw new BodyTooLargeError(429, { 'retry-after': '2' }); });
    expect(await retry.getMessage().invoke(call, { messageId: 'one' })).toMatchObject({ kind: 'retry', retryAfterMs: 2000 });
  });

  it('refreshes once after a 401 and never exposes either token in a result', async () => {
    let requests = 0;
    let refreshes = 0;
    const provider: CredentialSource & { resolveAfterRejection: (activeCall: Call, refreshDriver: CredentialRefreshDriver<Credentials>) => Promise<Credentials> } = {
      resolve: async () => credentials,
      resolveWithRefresh: async () => credentials,
      resolveAfterRejection: async () => { refreshes++; return { ...credentials, accessToken: new SecretString('replacement-token') }; },
    };
    const client = makeClient(async request => {
      requests++;
      if (requests === 1) { expect(request.headers?.Authorization).toBe('Bearer access-token'); return response(401, {}); }
      expect(request.headers?.Authorization).toBe('Bearer replacement-token');
      return response(404, {});
    }, provider);
    const result = await client.getMessage().invoke(call, { messageId: 'one' });
    expect(result).toMatchObject({ branch: 'notFound' });
    expect(requests).toBe(2);
    expect(refreshes).toBe(1);
    expect(JSON.stringify(result)).not.toContain('token');
  });

  it('also refreshes once when a 401 error body exceeds the response limit', async () => {
    let requests = 0;
    const provider: CredentialSource & { resolveAfterRejection: (activeCall: Call, refreshDriver: CredentialRefreshDriver<Credentials>) => Promise<Credentials> } = {
      resolve: async () => credentials,
      resolveWithRefresh: async () => credentials,
      resolveAfterRejection: async () => ({ ...credentials, accessToken: new SecretString('replacement-token') }),
    };
    const client = makeClient(async () => {
      requests++;
      if (requests === 1) throw new BodyTooLargeError(401, {});
      return response(404, {});
    }, provider);
    expect(await client.getMessage().invoke(call, { messageId: 'one' })).toMatchObject({ branch: 'notFound' });
    expect(requests).toBe(2);
  });

  it('uses the SDK providerhttp transport against a real loopback Gmail API fake', async () => {
    let authorization = '';
    const server = createServer((request, outgoing) => {
      authorization = request.headers.authorization ?? '';
      expect(request.url).toBe('/gmail/v1/users/me/messages/one?format=full');
      outgoing.setHeader('Content-Type', 'application/json');
      outgoing.end(JSON.stringify({ id: 'one', threadId: 'thread-1', internalDate: '1767312000000', payload: { mimeType: 'text/plain', body: { data: 'SGVsbG8' } } }));
    });
    server.listen(0, '127.0.0.1');
    await once(server, 'listening');
    try {
      const address = server.address();
      if (!address || typeof address === 'string') throw new Error('loopback server address is unavailable');
      const client = new GmailClient({ endpoint: `http://127.0.0.1:${address.port}/gmail/v1` }, new StaticCredentialProvider([[connection, credentials]]), { refreshDriver: driver });
      expect(await client.getMessage().invoke(call, { messageId: 'one' })).toMatchObject({ branch: 'read', value: { textBody: 'Hello' } });
      expect(authorization).toBe('Bearer access-token');
    } finally { await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve())); }
  });
});
