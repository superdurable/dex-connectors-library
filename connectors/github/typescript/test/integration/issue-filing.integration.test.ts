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

/** Local stand-in for the documented GitHub issue list, create, and user endpoints. */
class IssueProvider {
  readonly issues = new Map<number, Record<string, unknown>>();
  readonly sentBodies: string[] = [];
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
    const url = new URL(request.url ?? '/', 'http://127.0.0.1');
    const headers = { 'Content-Type': 'application/json', 'X-OAuth-Scopes': this.grantedScopes };
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
