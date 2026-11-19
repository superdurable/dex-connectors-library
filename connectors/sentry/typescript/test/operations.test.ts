// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { SentryClient } from '../src/client.js';
import { CredentialMaterialError } from '../src/generated/models.js';

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
    const missing = new SentryClient({}, { resolve: async () => { throw new ObjectNotFoundError(); } });
    const invalid = await missing.getIssue().invoke(call, { organization: 'acme', id: '123' });
    expect(invalid.kind === 'branch' && invalid.branch).toBe('defect');
    const unreadable = new SentryClient({}, { resolve: async () => { throw new Error('secret unavailable'); } });
    const unreadableResult = await unreadable.getIssue().invoke(call, { organization: 'acme', id: '123' });
    expect(unreadableResult.kind).toBe('retry');
    expect(JSON.stringify(unreadableResult)).not.toContain('secret unavailable');
    const refused = new SentryClient({ baseUrl: 'http://127.0.0.1:12345' }, {
      resolve: async () => ({ apiKey: new SecretString('test-secret-token') }),
    }, { send: async () => { throw new Error('connect ECONNREFUSED'); } });
    const retry = await refused.getIssue().invoke(call, { organization: 'acme', id: '123' });
    expect(retry.kind).toBe('retry');
  });
  it.each([
    ['reauthorization', () => new ReauthorizationRequiredError(), 'defect'],
    ['not selected', () => new ConnectionNotSelectedError(), 'defect'],
    ['broker rejected', () => new CredentialBrokerRejectedError(), 'defect'],
    ['malformed material', () => new CredentialMaterialError('credential api_key is required'), 'defect'],
    ['SDK decoding failure', () => new TypeError('project credential decoding failed'), 'defect'],
    ['broker unavailable', () => new CredentialBrokerUnavailableError(), 'retry'],
    ['not configured', () => new Error('connection is not configured'), 'retry'],
  ])('classifies a credential resolve failure (%s) for the status write', async (_name, newError, expected) => {
    const error = newError();
    const client = new SentryClient({}, { resolve: async () => { throw error; } }, { send: async () => { throw new Error('unexpected request'); } });
    const result = await client.updateIssueStatus().invoke(call, { organization: 'acme', id: '123', status: 'resolved' });
    expect(result.kind === 'branch' ? result.branch : result.kind).toBe(expected);
  });
  it.each([
    [409, '{"name":"invalid_idempotent_request"}', 'retry'],
    [425, '{}', 'retry'],
    [302, '', 'retry'],
    [200, '[]', 'retry'],
    [422, '{}', 'providerRejected'],
  ])('classifies status write HTTP %s as %s', async (status, body, expected) => {
    const client = new SentryClient({ baseUrl: 'http://127.0.0.1:12345' }, {
      resolve: async () => ({ apiKey: new SecretString('test-secret-token') }),
    }, { send: async () => ({ status, headers: {}, body: Buffer.from(body) }) });
    const result = await client.updateIssueStatus().invoke(call, { organization: 'acme', id: '123', status: 'resolved' });
    expect(result.kind === 'branch' ? result.branch : result.kind).toBe(expected);
  });
  it('encodes search query bytes like Go url.QueryEscape', async () => {
    let request: ProviderRequest | undefined;
    const client = new SentryClient({ baseUrl: 'http://127.0.0.1:12345' }, {
      resolve: async () => ({ apiKey: new SecretString('test-secret-token') }),
    }, { send: async sent => { request = sent; return { status: 200, headers: {}, body: Buffer.from('[]') }; } });
    await client.listIssues().invoke(call, { organization: 'acme', query: 'error *~' });
    expect(request?.url).toBe('http://127.0.0.1:12345/organizations/acme/issues/?query=error+%2A~');
  });
});
