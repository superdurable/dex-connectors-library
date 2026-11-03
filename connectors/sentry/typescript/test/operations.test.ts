// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { SentryClient } from '../src/client.js';

const call = { id: 'test-call', context: { cancellationSignal: new AbortController().signal } } as Call;

describe('Sentry provider adapter', () => {
  it('escapes path segments like Go url.PathEscape and never puts credentials in the URL', async () => {
    let request: ProviderRequest | undefined;
    const client = new SentryClient({ baseUrl: 'http://127.0.0.1:12345' }, {
      resolve: async () => ({ apiKey: new SecretString('test-secret-token') }),
    }, { send: async sent => { request = sent; return { status: 200, headers: {}, body: Buffer.from('{"id":"123"}') }; } });
    const result = await client.getIssue().invoke(call, { organization: "a!$&'()*+,;=:@/[]", id: 'issue/1' });
    expect(result.kind === 'branch' && result.branch).toBe('found');
    expect(request?.url).toBe('http://127.0.0.1:12345/organizations/a%21$&%27%28%29%2A+%2C%3B=:@%2F%5B%5D/issues/issue%2F1/');
    expect(request?.url).not.toContain('test-secret-token');
  });
  it('keeps missing credentials terminal and a refused transport retryable', async () => {
    const missing = new SentryClient({}, { resolve: async () => { throw new Error('secret unavailable'); } });
    const invalid = await missing.getIssue().invoke(call, { organization: 'acme', id: '123' });
    expect(invalid.kind === 'branch' && invalid.branch).toBe('defect');
    const refused = new SentryClient({ baseUrl: 'http://127.0.0.1:12345' }, {
      resolve: async () => ({ apiKey: new SecretString('test-secret-token') }),
    }, { send: async () => { throw new Error('connect ECONNREFUSED'); } });
    const retry = await refused.getIssue().invoke(call, { organization: 'acme', id: '123' });
    expect(retry.kind).toBe('retry');
  });
});
