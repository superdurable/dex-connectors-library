// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { GitLabClient, pathEscape } from '../src/client.js';

const call = { id: 'test', connection: { provider: 'gitlab', name: 'gitlab' }, operation: { connectorId: 'gitlab', operationId: 'getProject' }, context: { cancellationSignal: new AbortController().signal } } as Call;
const credentials = { resolve: async () => ({ accessToken: new SecretString('test-accesstoken') }) };

it('escapes one GitLab path segment like Go', () => {
  expect(pathEscape("a!$&'()*+,/:;=@?~")).toBe('a%21$&%27%28%29%2A+%2C%2F:%3B=@%3F~');
});

it('does not send a request when credentials are unavailable', async () => {
  const client = new GitLabClient({}, { resolve: async () => { throw new Error('secret'); } }, { send: async () => { throw new Error('unexpected request'); } });
  const attempt = await client.getProject().invoke(call, { projectId: '42' });
  expect(attempt).toMatchObject({ branch: 'defect', failure: { kind: 'AUTHENTICATION' } });
  expect(JSON.stringify(attempt)).not.toContain('secret');
});

it('retries a refused local connection', async () => {
  const client = new GitLabClient({}, credentials, { send: async () => { throw new Error('ECONNREFUSED'); } });
  const attempt = await client.getProject().invoke(call, { projectId: '42' });
  expect(attempt).toMatchObject({ failure: { kind: 'AVAILABILITY' } });
});

it.each([
  [503, 'retry', 'AVAILABILITY'],
  [404, 'notFound', 'NOT_FOUND'],
  [400, 'providerRejected', 'VALIDATION'],
  [200, 'invalidResponse', 'RESPONSE_TOO_LARGE'],
] as const)('classifies an oversized HTTP %i response by status', async (status, branch, kind) => {
  const client = new GitLabClient({}, credentials, { send: async () => { throw new BodyTooLargeError(status, {}); } });
  const attempt = await client.getProject().invoke(call, { projectId: '42' });
  expect(attempt).toMatchObject(branch === 'retry' ? { kind: 'retry', failure: { kind } } : { branch, failure: { kind } });
});

it('preserves dot segments in the URL passed to the provider sender', async () => {
  let request: ProviderRequest | undefined;
  const client = new GitLabClient({}, credentials, { send: async sentRequest => {
    request = sentRequest;
    return { status: 404, headers: {}, body: new Uint8Array() };
  } });
  await client.updateIssueLabels().invoke(call, { projectId: '..', issueIid: 7, labels: [] });
  expect(request?.url).toBe('https://gitlab.com/api/v4/projects/../issues/7');
});

it.each([undefined, null])('clears labels when labels are %s', async labels => {
  let request: ProviderRequest | undefined;
  const client = new GitLabClient({}, credentials, { send: async sentRequest => {
    request = sentRequest;
    return { status: 404, headers: {}, body: new Uint8Array() };
  } });
  await client.updateIssueLabels().invoke(call, { projectId: '42', issueIid: 7, labels: labels as unknown as string[] });
  expect(Buffer.from(request?.body ?? []).toString('utf8')).toBe('labels=');
});
