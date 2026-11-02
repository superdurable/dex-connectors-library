// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { GitLabClient, pathEscape } from '../src/client.js';

const call = { id: 'test', connection: { provider: 'gitlab', name: 'gitlab' }, operation: { connectorId: 'gitlab', operationId: 'getProject' }, context: { cancellationSignal: new AbortController().signal } } as Call;

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
  const client = new GitLabClient({}, { resolve: async () => ({ accessToken: new SecretString('test-accesstoken') }) }, { send: async () => { throw new Error('ECONNREFUSED'); } });
  const attempt = await client.getProject().invoke(call, { projectId: '42' });
  expect(attempt).toMatchObject({ failure: { kind: 'AVAILABILITY' } });
});
