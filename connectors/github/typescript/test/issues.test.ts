// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { CredentialBrokerUnavailableError } from '@superdurable/connectors-sdk/projectconfig';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { GitHubClient, PublicAuthMethodID, RepositoryAuthMethodID } from '../src/client.js';
import { CreateIssueCommentDefinition, CreateIssueDefinition, ReplaceIssueLabelsDefinition, SetIssueStateDefinition } from '../src/generated/definitions.js';
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

it('returns Retry without sending for an untrusted checkpoint', async () => {
  const requests: string[] = [];
  const context = new FenceContext();
  context.attempt = 2;
  context.marker = { callId: 'another-call', attempt: 1, state: 'notSent' };
  const call = { id: callId, connection, operation: { connectorId: 'github', operationId: 'createIssue' }, context } as unknown as Call;
  expect((await client([201], requests).createIssue().invoke(call, { owner: 'octocat', repository: 'hello-world', title: 'Crash' })).kind).toBe('retry');
  expect(requests).toHaveLength(0);
});

it('records canResend for a definite first-send refusal and treats an unlisted status as unknown', async () => {
  const input = { owner: 'octocat', repository: 'hello-world', title: 'Crash' };
  const refusedContext = new FenceContext();
  const refused = await client([422], []).createIssue().invoke({ id: callId, connection, operation: { connectorId: 'github', operationId: 'createIssue' }, context: refusedContext } as unknown as Call, input);
  expect(refused).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
  expect(refusedContext.marker).toMatchObject({ state: 'canResend' });
  const gone = await client([410], []).createIssue().invoke({ id: callId, connection, operation: { connectorId: 'github', operationId: 'createIssue' }, context: new FenceContext() } as unknown as Call, input);
  expect(gone.kind).toBe('retry');
});

it('treats an oversized or interrupted refusal body as an unknown outcome', async () => {
  const input = { owner: 'octocat', repository: 'hello-world', title: 'Crash' };
  for (const operationId of ['createIssue', 'createIssueComment'] as const) {
    for (const broken of [new BodyTooLargeError(422, {}), new ProviderTransportError(false, 422, {})]) {
      const requests: string[] = [];
      const github = new GitHubClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider([[connection, { authMethodId: RepositoryAuthMethodID, accessToken: new SecretString('token'), oauthClientId: 'id', oauthClientSecret: new SecretString('secret') }]]), {
        send: async (request: ProviderRequest) => {
          requests.push(request.method);
          if (request.method === 'POST') throw broken;
          return { status: 200, headers: { 'x-oauth-scopes': 'repo' }, body: new TextEncoder().encode(JSON.stringify(new URL(request.url).pathname === '/user' ? { login: 'octocat' } : [])) };
        },
      });
      const run = (context: FenceContext, maxSendsOverride?: number) => {
        const call = { id: callId, connection, operation: { connectorId: 'github', operationId }, context, ...(maxSendsOverride ? { maxSendsOverride } : {}) } as unknown as Call;
        return operationId === 'createIssue' ? github.createIssue().invoke(call, input) : github.createIssueComment().invoke(call, { owner: 'octocat', repository: 'hello-world', issueNumber: 7, body: 'Note' });
      };
      const context = new FenceContext();
      expect((await run(context)).kind).toBe('retry');
      expect(context.marker).toMatchObject({ state: 'claimed' });
      expect((await run(new FenceContext(), 1)).kind).toBe('uncertain');
      expect(requests.filter(method => method === 'POST')).toHaveLength(2);
    }
  }
});

it('never resends an issue whose 2xx body stalled', async () => {
  const requests: string[] = [];
  const github = new GitHubClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider([[connection, { authMethodId: RepositoryAuthMethodID, accessToken: new SecretString('token'), oauthClientId: 'id', oauthClientSecret: new SecretString('secret') }]]), {
    send: async (request: ProviderRequest) => {
      requests.push(request.method);
      if (request.method === 'POST') throw new ProviderStalledError(201, {});
      return { status: 200, headers: { 'x-oauth-scopes': 'repo' }, body: new TextEncoder().encode(JSON.stringify(new URL(request.url).pathname === '/user' ? { login: 'octocat' } : [])) };
    },
  });
  const call = { id: callId, connection, operation: { connectorId: 'github', operationId: 'createIssue' }, context: new FenceContext() } as unknown as Call;
  expect((await github.createIssue().invoke(call, { owner: 'octocat', repository: 'hello-world', title: 'Crash' })).kind).toBe('uncertain');
  expect(requests.filter(method => method === 'POST')).toHaveLength(1);
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

it('reads back after a 429 that followed an unknown send', async () => {
  const requests: string[] = [];
  const context = new FenceContext();
  const github = client([502, 429], requests);
  const input = { owner: 'octocat', repository: 'hello-world', title: 'Crash' };
  const call = () => ({ id: callId, connection, operation: { connectorId: 'github', operationId: 'createIssue' }, context }) as unknown as Call;
  for (let attempt = 1; attempt <= 2; attempt++, context.attempt++) expect((await github.createIssue().invoke(call(), input)).kind).toBe('retry');
  expect(context.marker).toMatchObject({ state: 'canResend' });
  requests.length = 0;
  await github.createIssue().invoke(call(), input);
  expect(requests.slice(0, 3)).toEqual(['GET /user', 'GET /repos/octocat/hello-world/issues', 'POST /repos/octocat/hello-world/issues']);
});

it('retries a transient credential failure after a possible send and selects uncertain for a revoked one', async () => {
  const requests: string[] = [];
  const context = new FenceContext();
  const input = { owner: 'octocat', repository: 'hello-world', title: 'Crash' };
  const call = () => ({ id: callId, connection, operation: { connectorId: 'github', operationId: 'createIssue' }, context }) as unknown as Call;
  expect((await client([502], requests).createIssue().invoke(call(), input)).kind).toBe('retry');
  context.attempt++;
  const failing = (error: Error) => new GitHubClient({ baseUrl: 'http://127.0.0.1:12345' }, { resolve: async () => { throw error; }, resolveWithRefresh: async () => { throw error; } }, { send: async () => { throw new Error('no request may be sent'); } });
  expect((await failing(new CredentialBrokerUnavailableError()).createIssue().invoke(call(), input)).kind).toBe('retry');
  expect((await failing(new ReauthorizationRequiredError()).createIssue().invoke(call(), input)).kind).toBe('uncertain');
});

it('retries a repeat-safe write whose answer is oversized or unusable', async () => {
  const input = { owner: 'octocat', repository: 'hello-world', issueNumber: 7, state: 'closed' };
  for (const send of [async () => { throw new BodyTooLargeError(200, {}); }, async () => ({ status: 200, headers: {}, body: new TextEncoder().encode('{"number":8}') })]) {
    const github = new GitHubClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider([[connection, { authMethodId: RepositoryAuthMethodID, accessToken: new SecretString('token'), oauthClientId: 'id', oauthClientSecret: new SecretString('secret') }]]), { send });
    expect((await github.setIssueState().invoke(stateCall(), input)).kind).toBe('retry');
    expect((await github.replaceIssueLabels().invoke(stateCall(), { owner: 'octocat', repository: 'hello-world', issueNumber: 7, labels: ['bug'] })).kind).toBe('retry');
  }
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

function labelsClient(statuses: number[], sent: string[], baseUrl = 'http://127.0.0.1:12345'): GitHubClient {
  const credentials = new StaticCredentialProvider([[connection, { authMethodId: RepositoryAuthMethodID, accessToken: new SecretString('token'), oauthClientId: 'id', oauthClientSecret: new SecretString('secret') }]]);
  const send = async (request: ProviderRequest) => {
    const body = new TextDecoder().decode(request.body);
    sent.push(`${request.method} ${new URL(request.url).pathname} ${body}`);
    const status = statuses.shift() ?? 200;
    if (status !== 200) return { status, headers: {}, body: new Uint8Array() };
    const labels = (JSON.parse(body) as { labels: string[] }).labels.map((name, index) => ({ id: index + 1, name }));
    return { status: 200, headers: {}, body: new TextEncoder().encode(JSON.stringify(labels)) };
  };
  return new GitHubClient({ baseUrl }, credentials, { send });
}

it('replaces issue labels with the full set and repeats it after an unknown outcome', async () => {
  const sent: string[] = [];
  const github = labelsClient([502], sent, 'https://api.github.com');
  const input = { owner: 'octocat', repository: 'hello-world', issueNumber: 7, labels: ['bug', ' triaged '] };
  expect((await github.replaceIssueLabels().invoke(stateCall(), input)).kind).toBe('retry');
  const replaced = await github.replaceIssueLabels().invoke(stateCall(), input);
  expect(replaced).toMatchObject({ kind: 'branch', branch: 'replaced', value: { number: 7, labels: ['bug', 'triaged'], url: 'https://github.com/octocat/hello-world/issues/7' } });
  expect(sent).toEqual(Array(2).fill('PUT /repos/octocat/hello-world/issues/7/labels {"labels":["bug","triaged"]}'));
  expect(ReplaceIssueLabelsDefinition.repeatOnUnknown).toBe('safe');
});

it('derives the issue URL only for GitHub.com and GitHub Enterprise Server API URLs', async () => {
  for (const [baseUrl, url] of [['https://github.example.com/api/v3', 'https://github.example.com/octocat/hello-world/issues/7'], ['https://proxy.example.com/github', undefined]] as const) {
    const attempt = await labelsClient([], [], baseUrl).replaceIssueLabels().invoke(stateCall(), { owner: 'octocat', repository: 'hello-world', issueNumber: 7, labels: [] });
    expect(attempt.kind === 'branch' ? attempt.value.url : 'not a branch').toBe(url);
  }
});

it('rejects invalid label input before any request', async () => {
  const sent: string[] = [];
  for (const labels of [[' '], ['a'.repeat(51)], Array(101).fill('bug')]) {
    const attempt = await labelsClient([], sent).replaceIssueLabels().invoke(stateCall(), { owner: 'octocat', repository: 'hello-world', issueNumber: 7, labels });
    expect(attempt).toMatchObject({ kind: 'branch', branch: 'defect' });
  }
  expect(sent).toEqual([]);
});

it('declares createIssueComment limited with the manifest send cap', () => {
  expect(CreateIssueCommentDefinition.repeatOnUnknown).toBe('limited');
  expect(CreateIssueCommentDefinition.maxSends).toBe(3);
});

it('reads back the first and newest comment pages before sending a comment again', async () => {
  const requests: string[] = [];
  const credentials = new StaticCredentialProvider([[connection, { authMethodId: RepositoryAuthMethodID, accessToken: new SecretString('token'), oauthClientId: 'id', oauthClientSecret: new SecretString('secret') }]]);
  const send = async (request: ProviderRequest) => {
    const url = new URL(request.url);
    requests.push(`${request.method} ${url.pathname}${url.search}`);
    if (request.method === 'POST') return { status: 502, headers: {}, body: new Uint8Array() };
    if (url.pathname === '/user') return { status: 200, headers: {}, body: new TextEncoder().encode('{"login":"octocat"}') };
    const headers = url.searchParams.get('page') === '1' ? { link: '<https://api.github.com/repositories/1/issues/7/comments?page=4>; rel="last"' } : {};
    return { status: 200, headers, body: new TextEncoder().encode('[]') };
  };
  const github = new GitHubClient({ baseUrl: 'http://127.0.0.1:12345' }, credentials, { send });
  const context = new FenceContext();
  const call = { id: callId, connection, operation: { connectorId: 'github', operationId: 'createIssueComment' }, context } as unknown as Call;
  const attempt = await github.createIssueComment().invoke(call, { owner: 'octocat', repository: 'hello-world', issueNumber: 7, body: 'Reproduced.' });
  expect(attempt.kind).toBe('retry');
  expect(requests).toEqual(['POST /repos/octocat/hello-world/issues/7/comments', 'GET /user',
    'GET /repos/octocat/hello-world/issues/7/comments?page=1&per_page=100', 'GET /repos/octocat/hello-world/issues/7/comments?page=4&per_page=100',
    'GET /repos/octocat/hello-world/issues/7/comments?page=3&per_page=100', 'GET /repos/octocat/hello-world/issues/7/comments?page=2&per_page=100']);
});
