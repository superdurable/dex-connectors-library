// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { JiraClient, pathEscape, encodeQuery } from '../src/client.js';

const connection = { provider: 'atlassian', name: 'jira-test' };
const call: Call = { id: 'jira-unit-call', connection, operation: { connectorId: 'jira', operationId: 'getIssue' }, context: { flowId: 'jira-unit', cancellationSignal: new AbortController().signal } as Call['context'], reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
const credentials = { oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret'), accessToken: new SecretString('jira-access-token'), refreshToken: new SecretString('refresh-token') };
const source = { resolve: async () => credentials, resolveWithRefresh: async () => credentials };
const cloudId = '11223344-a1b2-4b33-8c44-def123456789';
const issue = (created: unknown, other: Record<string, unknown> = {}) => ({ id: '10042', key: 'OPS-42', fields: { summary: 'Light out', status: { id: '1', name: 'To Do' }, issuetype: { id: '10001', name: 'Task', ...other }, project: { id: '10000', key: 'OPS' }, created, updated: '2026-09-30T10:00:00Z' } });
const client = (response: ProviderResponse, observe?: (request: ProviderRequest) => void) => new JiraClient({ endpoint: 'http://127.0.0.1', cloudId }, source, { send: async request => { observe?.(request); return response; } });

it('escapes paths and queries exactly as Go net/url', () => {
  expect(pathEscape("A!$&+()*,;?/@:~=é")).toBe('A%21$&+%28%29%2A%2C%3B%3F%2F@:~=%C3%A9');
  expect(encodeQuery({ z: 'a b~*', a: 'x+y' })).toBe('a=x%2By&z=a+b%7E%2A');
});

it('keeps rate-limit status before the successful-body size limit', async () => {
  let request: ProviderRequest | undefined;
  const provider = client({ status: 429, headers: { 'retry-after': '3' }, body: Buffer.alloc(1000) }, value => { request = value; });
  const attempt = await provider.getIssue().invoke(call, { issueIdOrKey: 'OPS-42' });
  expect(attempt.kind).toBe('retry');
  if (attempt.kind !== 'retry') throw new TypeError('expected retry');
  expect(attempt.failure.kind).toBe('RATE_LIMIT');
  expect(attempt.retryAfterMs).toBe(3000);
  expect(request?.url).toContain(`/ex/jira/${cloudId}/rest/api/3/issue/OPS-42?fields=`);
});

it('rejects invalid calendar dates and wrong-typed issue fields', async () => {
  for (const body of [issue('2026-02-30T09:00:00Z'), issue('2025-02-29T09:00:00Z'), issue('2026-09-30T24:00:00Z'), issue('2026-09-30T09:00:00Z', { subtask: 'yes' })]) {
    const provider = client({ status: 200, headers: {}, body: Buffer.from(JSON.stringify(body)) });
    const attempt = await provider.getIssue().invoke(call, { issueIdOrKey: 'OPS-42' });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind !== 'branch') throw new TypeError('expected invalid response branch');
    expect(attempt.branch).toBe('invalidResponse');
  }
});

it('accepts Go-supported large offsets and comma fractional seconds', async () => {
  for (const timestamp of ['2026-09-30T09:00:00+9959', '2026-09-30T09:00:00,123456789+01:30']) {
    const provider = client({ status: 200, headers: {}, body: Buffer.from(JSON.stringify(issue(timestamp))) });
    const attempt = await provider.getIssue().invoke(call, { issueIdOrKey: 'OPS-42' });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind !== 'branch') throw new TypeError('expected found branch');
    expect(attempt.branch, timestamp).toBe('found');
  }
});

it('rejects invalid connection settings before sending a request', () => {
  expect(() => new JiraClient({ cloudId: 'bad' }, source)).toThrow();
  expect(() => new JiraClient({ endpoint: 'http://example.com' }, source)).toThrow();
  expect(() => new JiraClient({ maxResponseBytes: -1 }, source)).toThrow();
});

it('treats reauthorization as terminal and transient credential lookup as Retry', async () => {
  const { ReauthorizationRequiredError } = await import('@superdurable/connectors-sdk');
  const terminal = new JiraClient({ cloudId }, { resolve: async () => { throw new ReauthorizationRequiredError(); }, resolveWithRefresh: async () => { throw new ReauthorizationRequiredError(); } });
  const transient = new JiraClient({ cloudId }, { resolve: async () => { throw new Error('temporary'); }, resolveWithRefresh: async () => { throw new Error('temporary'); } });
  const first = await terminal.getIssue().invoke(call, { issueIdOrKey: 'OPS-42' });
  const second = await transient.getIssue().invoke(call, { issueIdOrKey: 'OPS-42' });
  expect(first.kind).toBe('branch');
  if (first.kind !== 'branch') throw new TypeError('expected defect branch');
  expect(first.branch).toBe('defect'); expect(first.failure?.kind).toBe('AUTHENTICATION');
  expect(second.kind).toBe('retry');
  if (second.kind !== 'retry') throw new TypeError('expected retry');
  expect(second.failure.kind).toBe('AVAILABILITY');
});

it('refreshes once after a rejected access token and sends the replacement', async () => {
  const sent: string[] = [];
  const replacement = { ...credentials, accessToken: new SecretString('replacement-token') };
  const providerSource = { resolve: async () => credentials, resolveWithRefresh: async () => credentials, resolveAfterRejection: async () => replacement };
  const provider = new JiraClient({ endpoint: 'http://127.0.0.1', cloudId }, providerSource, { send: async request => {
    sent.push(request.headers?.Authorization ?? '');
    return sent.length === 1 ? { status: 401, headers: {}, body: Buffer.from('{}') } : { status: 200, headers: {}, body: Buffer.from(JSON.stringify(issue('2026-09-30T09:00:00Z'))) };
  } });
  const attempt = await provider.getIssue().invoke(call, { issueIdOrKey: 'OPS-42' });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind !== 'branch') throw new TypeError('expected found branch');
  expect(attempt.branch).toBe('found');
  expect(sent).toEqual(['Bearer jira-access-token', 'Bearer replacement-token']);
});
