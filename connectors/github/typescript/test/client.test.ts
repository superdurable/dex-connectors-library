// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { GitHubClient, pathEscape, queryEscape, requestReceipt } from '../src/client.js';

const connection = { provider: 'github', name: 'signup' };
const credentials = new StaticCredentialProvider([[connection, { accessToken: new SecretString('test-access'), oauthClientId: 'test-client', oauthClientSecret: new SecretString('test-secret') }]]);
const call = { id: 'test-call', connection, context: { cancellationSignal: new AbortController().signal } } as Call;

describe('GitHub transport contract', () => {
  it('uses Go path and query escaping', () => {
    expect(pathEscape("a!$&'()*+,;/:@~")).toBe('a%21$&%27%28%29%2A+%2C%3B%2F:@~');
    expect(queryEscape("a b!*~'()")).toBe('a+b%21%2A%7E%27%28%29');
  });

  it('honors the configured timeout and accepts a loopback alias', async () => {
    let observed: ProviderRequest | undefined;
    const client = new GitHubClient({ baseUrl: 'http://127.0.0.2', timeoutMs: 2_500 }, credentials, {
      send: async request => { observed = request; return { status: 404, headers: {}, body: new Uint8Array() }; },
    });
    const result = await client.get(call, 'listReleases', '/repos/octocat/hello-world/releases');
    expect(observed?.timeoutMs).toBe(2_500);
    expect(observed?.addressPolicy).toBe('unrestricted');
    expect('branch' in result && result.branch).toBe('notFound');
  });

  it('falls back to the second valid request ID', () => {
    expect(requestReceipt({ 'x-github-request-id': 'bad id', 'x-request-id': 'valid-123' })).toEqual({ providerRequestId: 'valid-123' });
  });
});
