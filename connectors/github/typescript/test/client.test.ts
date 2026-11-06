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
    expect(queryEscape("a b!*~'()")).toBe('a+b%21%2A~%27%28%29');
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

  it.each(['1e3', '5.0', '0x10'])('rejects non-integer Retry-After %s', async retryAfter => {
    const client = new GitHubClient({}, credentials, {
      send: async () => ({ status: 429, headers: { 'retry-after': retryAfter }, body: new Uint8Array() }),
    });
    const result = await client.get(call, 'listReleases', '/repos/octocat/hello-world/releases');
    expect('retryAfterMs' in result && result.retryAfterMs).toBe(60_000);
  });
  it('reads the next page across every Link header value', async () => {
    const client = new GitHubClient({}, credentials, { send: async () => ({ status: 200,
      headers: { 'x-oauth-scopes': 'read:user, user:email', Link: ['<https://api.github.com/repos/a/b/releases?page=1>; rel="prev"', '<https://api.github.com/repos/a/b/releases?page=3>; rel="next"'] },
      body: Buffer.from('[]') }) });
    const result = await client.listReleases().invoke(call, { owner: 'octocat', repository: 'hello-world' });
    expect(result).toMatchObject({ kind: 'branch', branch: 'listed', value: { nextPage: 3 } });
  });

  it.each(['5.0', '1e3', '0x10'])('rejects non-integer next page %s', async page => {
    const client = new GitHubClient({}, credentials, { send: async () => ({ status: 200,
      headers: { 'x-oauth-scopes': 'read:user, user:email', Link: `<https://api.github.com/repos/a/b/releases?page=${page}>; rel="next"` }, body: Buffer.from('[]') }) });
    const result = await client.listReleases().invoke(call, { owner: 'octocat', repository: 'hello-world' });
    expect(result).toMatchObject({ kind: 'branch', branch: 'invalidResponse' });
  });

  it('falls back to the second valid request ID', () => {
    expect(requestReceipt({ 'x-github-request-id': 'bad id', 'x-request-id': 'valid-123' })).toEqual({ providerRequestId: 'valid-123' });
  });
});
