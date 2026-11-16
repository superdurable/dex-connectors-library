// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { GitHubClient, PublicAuthMethodID, RepositoryAuthMethodID } from '../src/client.js';
import { CreateIssueDefinition, SetIssueStateDefinition } from '../src/generated/definitions.js';
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

function stateClient(statuses: number[], sent: string[], authMethodId = RepositoryAuthMethodID): GitHubClient {
  const credentials = new StaticCredentialProvider([[connection, { authMethodId, accessToken: new SecretString('token'), oauthClientId: 'id', oauthClientSecret: new SecretString('secret') }]]);
  const send = async (request: ProviderRequest) => {
    const body = JSON.parse(new TextDecoder().decode(request.body)) as { state: string; state_reason?: string };
    sent.push(`${request.method} ${new URL(request.url).pathname} ${JSON.stringify(body)}`);
    const status = statuses.shift() ?? 200;
    if (status !== 200) return { status, headers: {}, body: new Uint8Array() };
    const issue = { id: 7, node_id: 'I_node7', number: 7, state: body.state, state_reason: body.state_reason ?? (body.state === 'closed' ? 'completed' : 'reopened'),
      html_url: 'https://github.com/octocat/hello-world/issues/7', created_at: '2026-03-07T10:15:00Z', updated_at: '2026-03-08T09:30:00Z' };
    return { status: 200, headers: {}, body: new TextEncoder().encode(JSON.stringify(issue)) };
  };
  return new GitHubClient({ baseUrl: 'http://127.0.0.1:12345' }, credentials, { send });
}

function stateCall(): Call {
  return { id: callId, connection, operation: { connectorId: 'github', operationId: 'setIssueState' }, context: new FenceContext() } as unknown as Call;
}

it('declares setIssueState safe to repeat', () => {
  expect(SetIssueStateDefinition.repeatOnUnknown).toBe('safe');
  expect(SetIssueStateDefinition.maxSends).toBeUndefined();
});

it('closes and reopens an issue with one PATCH each', async () => {
  const sent: string[] = [];
  const github = stateClient([], sent);
  const closed = await github.setIssueState().invoke(stateCall(), { owner: ' octocat ', repository: 'hello-world', issueNumber: 7, state: 'closed', stateReason: 'not_planned' });
  expect(closed).toMatchObject({ kind: 'branch', branch: 'updated', value: { number: 7, url: 'https://github.com/octocat/hello-world/issues/7', state: 'closed', stateReason: 'not_planned', updatedAt: '2026-03-08T09:30:00Z' } });
  const reopened = await github.setIssueState().invoke(stateCall(), { owner: 'octocat', repository: 'hello-world', issueNumber: 7, state: 'open' });
  expect(reopened).toMatchObject({ kind: 'branch', branch: 'updated', value: { state: 'open', stateReason: 'reopened' } });
  expect(sent).toEqual(['PATCH /repos/octocat/hello-world/issues/7 {"state":"closed","state_reason":"not_planned"}', 'PATCH /repos/octocat/hello-world/issues/7 {"state":"open"}']);
});

it('retries an unknown setIssueState outcome by sending the same state again', async () => {
  const sent: string[] = [];
  const github = stateClient([502], sent);
  const input = { owner: 'octocat', repository: 'hello-world', issueNumber: 7, state: 'closed' };
  expect((await github.setIssueState().invoke(stateCall(), input)).kind).toBe('retry');
  expect((await github.setIssueState().invoke(stateCall(), input)).kind).toBe('branch');
  expect(sent).toEqual(['PATCH /repos/octocat/hello-world/issues/7 {"state":"closed"}', 'PATCH /repos/octocat/hello-world/issues/7 {"state":"closed"}']);
});

it('selects insufficientScope for setIssueState on the public method without a request', async () => {
  const sent: string[] = [];
  const attempt = await stateClient([], sent, PublicAuthMethodID).setIssueState().invoke(stateCall(), { owner: 'octocat', repository: 'hello-world', issueNumber: 7, state: 'closed' });
  expect(attempt).toMatchObject({ kind: 'branch', branch: 'insufficientScope', value: { number: 7 } });
  expect(sent).toEqual([]);
});

it.each([
  { state: 'closed', stateReason: 'reopened' },
  { state: 'open', stateReason: 'completed' },
  { state: 'open', stateReason: 'not_planned' },
  { state: 'all' },
  { state: 'closed', issueNumber: 0 },
])('rejects setIssueState input %o before any request', async ({ issueNumber = 7, ...stateInput }) => {
  const sent: string[] = [];
  const attempt = await stateClient([], sent).setIssueState().invoke(stateCall(), { owner: 'octocat', repository: 'hello-world', issueNumber, ...stateInput });
  expect(attempt).toMatchObject({ kind: 'branch', branch: 'defect' });
  expect(sent).toEqual([]);
});
