// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { OAuth2HTTPClient } from '../src/client.js';

const connection = { provider: 'oauth2-http', name: 'tested-api' };
const firstCredentials = { oauthClientId: 'client', oauthClientSecret: new SecretString('client-secret'), accessToken: new SecretString('first-access-token'), refreshToken: new SecretString('') };
const config = { authorizationEndpoint: 'http://127.0.0.1:45880/auth', tokenEndpoint: 'http://127.0.0.1:45880/token', scopes: ['profile'], apiBaseUrl: 'http://127.0.0.1:45880/v1' };
const call = { id: 'b3c30387-4be9-5a02-9193-ce34ca4c38c8', idempotencyKey: 'b3c30387-4be9-5a02-9193-ce34ca4c38c8', connection,
  context: { cancellationSignal: new AbortController().signal } } as Call;

describe('sendRequest provider semantics', () => {
  it('refreshes once after a 401 and redacts both access tokens', async () => {
    const URLs: string[] = [];
    const credentials = {
      resolve: async () => firstCredentials,
      resolveWithRefresh: async () => firstCredentials,
      resolveAfterRejection: async () => ({ ...firstCredentials, accessToken: new SecretString('second-access-token') }),
    };
    const client = new OAuth2HTTPClient(config, credentials, { send: async request => {
      URLs.push(request.headers?.Authorization ?? '');
      return URLs.length === 1
        ? { status: 401, headers: {}, body: Buffer.from('expired') }
        : { status: 200, headers: { 'content-type': 'text/plain' }, body: Buffer.from('first-access-token then second-access-token') };
    } });
    const attempt = await client.sendRequest().invoke(call, { path: '/userinfo' });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind === 'branch') expect(attempt.value).toEqual({ statusCode: 200, contentType: 'text/plain', body: '[REDACTED] then [REDACTED]' });
    expect(URLs).toEqual(['Bearer first-access-token', 'Bearer second-access-token']);
  });

  it('uses Go query escaping and retries an unsent POST', async () => {
    let target = '';
    const client = new OAuth2HTTPClient(config, new StaticCredentialProvider([[connection, firstCredentials]]), { send: async request => {
      target = request.url;
      throw new ProviderTransportError(true);
    } });
    const attempt = await client.sendRequest().invoke(call, { method: 'POST', path: '/items', query: { 'z~': ['a b!*'], a: ['x'] }, body: 'item' });
    expect(attempt.kind).toBe('retry');
    if (attempt.kind === 'retry') expect(attempt.failure.kind).toBe('AVAILABILITY');
    expect(target).toBe('http://127.0.0.1:45880/v1/items?a=x&z%7E=a+b%21%2A');
  });

  it('returns a bounded prefix and drops an incomplete UTF-8 rune', async () => {
    const client = new OAuth2HTTPClient({ ...config, maxBodyBytes: 4 }, new StaticCredentialProvider([[connection, firstCredentials]]), { send: async request => {
      request.onResponseChunk?.(Buffer.from([0x41, 0x42, 0x43, 0xe2, 0x82]), 200, {});
      throw new BodyTooLargeError(200, { 'content-type': 'text/plain' });
    } });
    const attempt = await client.sendRequest().invoke(call, { path: '/document' });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind === 'branch') expect(attempt.value).toEqual({ statusCode: 200, contentType: 'text/plain', body: 'ABC', bodyTruncated: true });
  });
});
