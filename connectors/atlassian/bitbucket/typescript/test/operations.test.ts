// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BitbucketClient } from '../src/client.js';

const call = {
  id: 'd39fca4a-b5a9-5151-9985-884b5fb10087',
  connection: { provider: 'bitbucket', name: 'bitbucket' },
  operation: { connectorId: 'bitbucket', operationId: 'listPullRequests' },
  context: { cancellationSignal: new AbortController().signal },
} as Call;

it('escapes both repository path segments and keeps the token in the header', async () => {
  let requestedURL = '';
  let requestedToken = '';
  const client = new BitbucketClient({}, { resolve: async () => ({ accessToken: new SecretString('test-token') }) }, {
    send: async request => {
      requestedURL = request.url;
      requestedToken = request.headers?.Authorization ?? '';
      return { status: 200, headers: {}, body: Buffer.from('{"values":[{"id":7,"title":"Add tests","state":"OPEN"}],"page":1,"pagelen":10}') };
    },
  });
  const result = await client.listPullRequests().invoke(call, { workspace: 'meri dian', repository: 'api repo' });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') expect(result.branch).toBe('listed');
  expect(requestedURL).toBe('https://api.bitbucket.org/2.0/repositories/meri%20dian/api%20repo/pullrequests');
  expect(requestedURL).not.toContain('test-token');
  expect(requestedToken).toBe('Bearer test-token');
});

it('returns a local defect without sending malformed path segments', async () => {
  let requests = 0;
  const client = new BitbucketClient({}, { resolve: async () => ({ accessToken: new SecretString('test-token') }) }, {
    send: async () => { requests++; throw new Error('unexpected request'); },
  });
  const result = await client.listPullRequests().invoke(call, { workspace: 'meridian', repository: '../secret' });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') { expect(result.branch).toBe('defect'); expect(result.failure?.kind).toBe('VALIDATION'); }
  expect(requests).toBe(0);
});

it('rejects unsafe credentials before HTTP', async () => {
  let requests = 0;
  const client = new BitbucketClient({}, { resolve: async () => ({ accessToken: new SecretString('bad\ntoken') }) }, {
    send: async () => { requests++; throw new Error('unexpected request'); },
  });
  const result = await client.listRepositories().invoke(call, { workspace: 'meridian' });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') { expect(result.branch).toBe('defect'); expect(result.failure?.kind).toBe('AUTHENTICATION'); }
  expect(requests).toBe(0);
});
