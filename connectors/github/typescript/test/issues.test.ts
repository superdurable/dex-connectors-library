// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { GitHubClient, RepositoryAuthMethodID } from '../src/client.js';
import { CreateIssueDefinition } from '../src/generated/definitions.js';
import { issueCallMarker } from '../src/issues.js';

const connection = { provider: 'github', name: 'github' };
const callId = '8f014eab-9116-5e00-9eb0-008374ce7d9e';

class FenceContext {
  attempt = 1;
  marker: unknown;
  readonly cancellationSignal = new AbortController().signal;
  readonly deadline = new Date(Date.now() + 30_000);
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

function client(statuses: number[], requests: string[]): GitHubClient {
  const credentials = new StaticCredentialProvider([[connection, { authMethodId: RepositoryAuthMethodID, accessToken: new SecretString('token'), oauthClientId: 'id', oauthClientSecret: new SecretString('secret') }]]);
  const send = async (request: ProviderRequest) => {
    requests.push(`${request.method} ${new URL(request.url).pathname}`);
    if (request.method === 'POST') return { status: statuses.shift() ?? 502, headers: {}, body: new Uint8Array() };
    const body = new URL(request.url).pathname === '/user' ? { login: 'octocat' } : [];
    return { status: 200, headers: { 'x-oauth-scopes': 'repo' }, body: new TextEncoder().encode(JSON.stringify(body)) };
  };
  return new GitHubClient({ baseUrl: 'http://127.0.0.1:12345' }, credentials, { send });
}

it('derives the same marker as the Go connector', () => {
  expect(issueCallMarker(callId)).toBe('<!-- dex-connector-call: 56fd1cabcca66f3675335077d870dc199a98cd1bb76591c150f584fe24d8306a -->');
});

it('declares the limited tier and send cap from the manifest', () => {
  expect(CreateIssueDefinition.repeatOnUnknown).toBe('limited');
  expect(CreateIssueDefinition.maxSends).toBe(3);
});

it('never sends again with a Step maxSends of 1', async () => {
  const requests: string[] = [];
  const context = new FenceContext();
  const call = { id: callId, connection, operation: { connectorId: 'github', operationId: 'createIssue' }, context, maxSendsOverride: 1 } as unknown as Call;
  const attempt = await client([502], requests).createIssue().invoke(call, { owner: 'octocat', repository: 'hello-world', title: 'Crash' });
  expect(attempt.kind).toBe('uncertain');
  expect(requests.filter(request => request.startsWith('POST'))).toHaveLength(1);
});

it('reads back before sending again below the cap', async () => {
  const requests: string[] = [];
  const context = new FenceContext();
  const github = client([502, 503], requests);
  const input = { owner: 'octocat', repository: 'hello-world', title: 'Crash' };
  const call = () => ({ id: callId, connection, operation: { connectorId: 'github', operationId: 'createIssue' }, context }) as unknown as Call;
  expect((await github.createIssue().invoke(call(), input)).kind).toBe('retry');
  context.attempt++;
  expect((await github.createIssue().invoke(call(), input)).kind).toBe('retry');
  expect(requests).toEqual(['POST /repos/octocat/hello-world/issues', 'GET /user', 'GET /repos/octocat/hello-world/issues',
    'GET /user', 'GET /repos/octocat/hello-world/issues', 'POST /repos/octocat/hello-world/issues']);
});
