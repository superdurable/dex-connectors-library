// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { afterAll, beforeAll, expect, it } from 'vitest';
import { GitHubClient, PublicAuthMethodID, RepositoryAuthMethodID } from '../../src/client.js';
import { newConnection } from '../../src/index.js';
import { ConnectionName, IssueFilingFlow, type IssueFilingInput, type IssueFilingReport } from '../../examples/issue-filing/flow/workflow.js';

const token = 'github-integration-token';
type CreateBehavior = 'store201' | 'store502' | 'drop502';

/** Local stand-in for the documented GitHub issue list, create, update, label, comment, and user endpoints. */
class IssueProvider {
  readonly issues = new Map<number, Record<string, unknown>>();
  readonly sentBodies: string[] = [];
  readonly sentStateUpdates: string[] = [];
  requestCount = 0;
  /** State updates applied and then answered with 502. */
  failStateUpdates = 0;
  readonly comments: Record<string, unknown>[] = [];
  commentPosts = 0;
  /** Comments stored and then answered with 502. */
  failCommentPosts = 0;
  /** Comment listings answered with 502. */
  failCommentLists = 0;
  private nextCommentId = 7001;

  storeComment(issue: number, body: string, author: string): Record<string, unknown> {
    const id = this.nextCommentId++;
    const comment = { id, issue, body, user: { login: author }, html_url: `https://github.com/octocat/hello-world/issues/${issue}#issuecomment-${id}`,
      created_at: new Date().toISOString().replace(/\.\d{3}Z$/, 'Z') };
    this.comments.push(comment);
    return comment;
  }
  posts = 0;
  readBackLists = 0;
  failReadBackLists = 0;
  grantedScopes = 'repo';
  private nextNumber = 1;
  constructor(private behaviors: CreateBehavior[] = []) {}

  store(title: string, body: string, author: string, labels: string[]): Record<string, unknown> {
    const number = this.nextNumber++;
    const now = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');
    const issue = { id: 1000 + number, node_id: `I_node${number}`, number, title, body, state: 'open', html_url: `https://github.com/octocat/hello-world/issues/${number}`,
      user: { login: author }, labels: labels.map(name => ({ name })), assignees: [], comments: 0, created_at: now, updated_at: now };
    this.issues.set(number, issue);
    return issue;
  }

  async handle(request: IncomingMessage, response: ServerResponse): Promise<void> {
    if (request.headers.authorization !== `Bearer ${token}`) { response.writeHead(401).end('{"message":"Bad credentials"}'); return; }
    this.requestCount++;
    const url = new URL(request.url ?? '/', 'http://127.0.0.1');
    const headers = { 'Content-Type': 'application/json', 'X-OAuth-Scopes': this.grantedScopes };
    const commentsOn = /^\/repos\/octocat\/hello-world\/issues\/(\d+)\/comments$/.exec(url.pathname)?.[1];
    if (commentsOn) {
      const issue = Number(commentsOn);
      if (request.method === 'GET') {
        if (this.failCommentLists > 0) { this.failCommentLists--; response.writeHead(502, headers).end(); return; }
        const onIssue = this.comments.filter(comment => comment.issue === issue);
        const size = Number(url.searchParams.get('per_page')), page = Number(url.searchParams.get('page'));
        const lastPage = Math.ceil(onIssue.length / size);
        const pageHeaders = lastPage > 1 ? { ...headers, Link: `<https://api.github.com/repositories/1/issues/${issue}/comments?page=${lastPage}>; rel="last"` } : headers;
        response.writeHead(200, pageHeaders).end(JSON.stringify(onIssue.slice((page - 1) * size, page * size)));
        return;
      }
      let raw = '';
      for await (const chunk of request) raw += String(chunk);
      this.commentPosts++;
      const stored = this.storeComment(issue, (JSON.parse(raw) as { body: string }).body, 'octocat');
      if (this.failCommentPosts > 0) { this.failCommentPosts--; response.writeHead(502, headers).end(); return; }
      response.writeHead(201, headers).end(JSON.stringify(stored));
      return;
    }
    const labelsOn = /^\/repos\/octocat\/hello-world\/issues\/(\d+)\/labels$/.exec(url.pathname)?.[1];
    if (request.method === 'PUT' && labelsOn) {
      let raw = '';
      for await (const chunk of request) raw += String(chunk);
      const issue = this.issues.get(Number(labelsOn));
      if (!issue) { response.writeHead(404, headers).end('{"message":"Not Found"}'); return; }
      issue.labels = (JSON.parse(raw) as { labels: string[] }).labels.map(name => ({ name }));
      response.writeHead(200, headers).end(JSON.stringify(issue.labels));
      return;
    }
    const issueNumber = /^\/repos\/octocat\/hello-world\/issues\/(\d+)$/.exec(url.pathname)?.[1];
    if (request.method === 'PATCH' && issueNumber) {
      let raw = '';
      for await (const chunk of request) raw += String(chunk);
      this.sentStateUpdates.push(raw);
      const update = JSON.parse(raw) as { state: string; state_reason?: string };
      const issue = this.issues.get(Number(issueNumber));
      if (!issue) { response.writeHead(404, headers).end('{"message":"Not Found"}'); return; }
      Object.assign(issue, { state: update.state, state_reason: update.state_reason ?? null, updated_at: new Date().toISOString().replace(/\.\d{3}Z$/, 'Z') });
      if (this.failStateUpdates > 0) { this.failStateUpdates--; response.writeHead(502, headers).end(); return; }
      response.writeHead(200, headers).end(JSON.stringify(issue));
      return;
    }
    if (request.method === 'GET' && url.pathname === '/user') { response.writeHead(200, headers).end(JSON.stringify({ login: 'octocat', id: 1 })); return; }
    if (url.pathname !== '/repos/octocat/hello-world/issues') { response.writeHead(404, headers).end('{}'); return; }
    if (request.method === 'POST') {
      let raw = '';
      for await (const chunk of request) raw += String(chunk);
      const sent = JSON.parse(raw) as { title: string; body: string; labels?: string[] };
      this.posts++;
      this.sentBodies.push(sent.body);
      const behavior = this.behaviors.shift() ?? 'store201';
      const stored = behavior === 'drop502' ? undefined : this.store(sent.title, sent.body, 'octocat', sent.labels ?? []);
      if (behavior !== 'store201') { response.writeHead(502, headers).end(); return; }
      response.writeHead(201, headers).end(JSON.stringify(stored));
      return;
    }
    const query = url.searchParams;
    if (query.get('creator')) {
      this.readBackLists++;
      if (this.failReadBackLists > 0) { this.failReadBackLists--; response.writeHead(502, headers).end(); return; }
    }
    const labels = query.get('labels') ? query.get('labels')!.split(',') : [];
    const matching = [...this.issues.values()].filter(issue =>
      (query.get('state') === 'all' || issue.state === query.get('state')) &&
      labels.every(label => (issue.labels as { name: string }[]).some(value => value.name === label)) &&
      (!query.get('creator') || (issue.user as { login: string }).login === query.get('creator')))
      .sort((left, right) => Number(right.number) - Number(left.number));
    const size = Number(query.get('per_page')), page = Number(query.get('page'));
    const pageItems = matching.slice((page - 1) * size, page * size);
    if (page * size < matching.length) Object.assign(headers, { Link: `<https://api.github.com/repositories/1/issues?page=${page + 1}>; rel="next"` });
    response.writeHead(200, headers).end(JSON.stringify(pageItems));
  }
}

const bugReport: IssueFilingInput = { owner: 'octocat', repository: 'hello-world', title: 'Crash when saving an empty draft', body: 'Saving a new draft without text closes the editor.', labels: ['bug'] };
let directory = '';
let dexAddress = '';
beforeAll(async () => {
  dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS ?? '';
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  directory = await mkdtemp(join(tmpdir(), 'dex-github-issue-filing-'));
});
afterAll(async () => { if (directory) await rm(directory, { recursive: true, force: true }); });

async function runFiling(provider: IssueProvider, authMethodId: string, input: IssueFilingInput) {
  const server = createServer((request, response) => { void provider.handle(request, response); });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('GitHub stand-in port unavailable');
  const cache = openBlobCache({ directory: join(directory, randomUUID()), maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'github', name: ConnectionName };
  const credentials = new StaticCredentialProvider([[connection, { authMethodId, accessToken: new SecretString(token), oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret') }]]);
  const flow = new IssueFilingFlow(newConnection(new GitHubClient({ baseUrl: `http://127.0.0.1:${address.port}` }, credentials), connection));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `github-issue-filing-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    return await client.waitForFlow(flowId, 90_000);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}

it('creates the issue once after checking every open page', async () => {
  const provider = new IssueProvider();
  for (let index = 0; index < 150; index++) provider.store(`Unrelated report ${index}`, 'Details', 'hubot', ['bug']);
  const result = await runFiling(provider, RepositoryAuthMethodID, bugReport);
  expect(result.status, result.errorMessage).toBe('completed');
  const report = result.singleOutput<IssueFilingReport>();
  expect(report.status).toBe('created');
  expect(report.openIssuePagesRead).toBe(2);
  expect(report.createdIssue?.number).toBe(151);
  expect(report.createdIssue?.labels).toEqual(['bug']);
  expect(provider.posts).toBe(1);
  expect(provider.sentBodies[0]).toMatch(/^Saving a new draft without text closes the editor\.\n\n<!-- dex-connector-call: [0-9a-f]{64} -->$/);
  expect(JSON.stringify(report)).not.toContain(token);
}, 120_000);

it('finds an already tracked title and creates nothing', async () => {
  const provider = new IssueProvider();
  provider.store('  crash when saving an EMPTY draft ', 'Reported earlier', 'hubot', ['bug']);
  for (let index = 0; index < 120; index++) provider.store(`Unrelated report ${index}`, 'Details', 'hubot', ['bug']);
  const result = await runFiling(provider, RepositoryAuthMethodID, bugReport);
  expect(result.status, result.errorMessage).toBe('completed');
  const report = result.singleOutput<IssueFilingReport>();
  expect(report.status).toBe('alreadyTracked');
  expect(report.existingIssue?.number).toBe(1);
  expect(provider.posts).toBe(0);
}, 120_000);

it('reads back a retried create instead of filing a duplicate', async () => {
  const provider = new IssueProvider(['store502']);
  provider.failReadBackLists = 1;
  const result = await runFiling(provider, RepositoryAuthMethodID, bugReport);
  expect(result.status, result.errorMessage).toBe('completed');
  expect(result.singleOutput<IssueFilingReport>().createdIssue?.number).toBe(1);
  expect(provider.posts).toBe(1);
  expect(provider.readBackLists).toBe(2);
  expect([...provider.issues.values()].filter(issue => issue.title === bugReport.title)).toHaveLength(1);
}, 120_000);

it('resends only after read-back finds no issue', async () => {
  const provider = new IssueProvider(['drop502']);
  const result = await runFiling(provider, RepositoryAuthMethodID, bugReport);
  expect(result.status, result.errorMessage).toBe('completed');
  expect(provider.posts).toBe(2);
  expect(provider.sentBodies[0]).toBe(provider.sentBodies[1]);
  expect([...provider.issues.values()].filter(issue => issue.title === bugReport.title)).toHaveLength(1);
}, 120_000);

it('fails on a public connection without writing', async () => {
  const provider = new IssueProvider();
  provider.grantedScopes = 'read:user, user:email';
  const result = await runFiling(provider, PublicAuthMethodID, bugReport);
  expect(result.status).toBe('failed');
  expect(result.errorMessage).toContain('insufficientScope');
  expect(provider.posts).toBe(0);
}, 120_000);

it('closes the issue it filed, sending the same state again after a 502', async () => {
  const provider = new IssueProvider();
  provider.failStateUpdates = 1;
  const result = await runFiling(provider, RepositoryAuthMethodID, { ...bugReport, closeWithStateReason: 'not_planned' });
  expect(result.status, result.errorMessage).toBe('completed');
  const report = result.singleOutput<IssueFilingReport>();
  expect(report.status).toBe('closed');
  expect(report.createdIssue?.number).toBe(1);
  expect(report.closedIssue).toMatchObject({ number: 1, state: 'closed', stateReason: 'not_planned', url: 'https://github.com/octocat/hello-world/issues/1' });
  expect(report.closedIssue?.updatedAt).toBeTruthy();
  expect(provider.posts).toBe(1);
  expect(provider.sentStateUpdates).toEqual(['{"state":"closed","state_reason":"not_planned"}', '{"state":"closed","state_reason":"not_planned"}']);
  expect(provider.issues.get(1)).toMatchObject({ state: 'closed', state_reason: 'not_planned' });
}, 120_000);

it('rejects a reopen reason before any request', async () => {
  const provider = new IssueProvider();
  const result = await runFiling(provider, RepositoryAuthMethodID, { ...bugReport, closeWithStateReason: 'reopened' });
  expect(result.status).toBe('failed');
  expect(result.errorMessage).toContain('closeWithStateReason');
  expect(provider.requestCount).toBe(0);
}, 120_000);

it('comments on, relabels, and closes the issue it filed, reading back a retried comment', async () => {
  const provider = new IssueProvider();
  provider.failCommentPosts = 1;
  provider.failCommentLists = 1;
  for (let index = 0; index < 120; index++) provider.storeComment(1, 'Another reply', 'hubot');
  const result = await runFiling(provider, RepositoryAuthMethodID, { ...bugReport, comment: 'Fixed by the draft validation change.', relabelWith: ['bug', 'fixed'], closeWithStateReason: 'completed' });
  expect(result.status, result.errorMessage).toBe('completed');
  const report = result.singleOutput<IssueFilingReport>();
  expect(report.status).toBe('closed');
  expect(report.issueComment).toMatchObject({ issueNumber: 1 });
  expect(report.relabeledIssue).toMatchObject({ number: 1, labels: ['bug', 'fixed'] });
  expect(report.closedIssue).toMatchObject({ state: 'closed', stateReason: 'completed' });
  const own = provider.comments.filter(comment => comment.issue === 1 && (comment.user as { login: string }).login === 'octocat');
  expect(own).toHaveLength(1);
  expect(provider.commentPosts).toBe(1);
  expect(own[0]!.body).toMatch(/^Fixed by the draft validation change\.\n\n<!-- dex-connector-call: [0-9a-f]{64} -->$/);
  expect(provider.issues.get(1)).toMatchObject({ labels: [{ name: 'bug' }, { name: 'fixed' }], state: 'closed' });
}, 120_000);
