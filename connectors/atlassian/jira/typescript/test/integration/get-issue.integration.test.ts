// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { JiraClient } from '../../src/client.js';
import { newConnection, type Issue } from '../../src/index.js';
import { ConnectionName, GetJiraIssueFlow } from '../../examples/get-issue/flow/workflow.js';

it('reads an issue through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (!request.url?.startsWith('/ex/jira/11223344-a1b2-4b33-8c44-def123456789/rest/api/3/issue/')) { response.writeHead(404).end(); return; }
    if (request.headers.authorization !== 'Bearer jira-access-token') { response.writeHead(401).end(); return; }
    if (request.url.includes('OPS-403')) { response.writeHead(403, { 'Content-Type': 'application/json' }).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ id: '10042', key: 'OPS-42', fields: { summary: 'Loading dock light out', status: { id: '1', name: 'To Do', statusCategory: { key: 'new' } }, issuetype: { id: '10001', name: 'Task' }, project: { id: '10000', key: 'OPS', name: 'Operations' }, labels: ['facilities'], created: '2026-09-30T09:15:00.000-0700', updated: '2026-09-30T10:00:00.000-0700' } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Jira fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-jira-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials = { oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret'), accessToken: new SecretString('jira-access-token'), refreshToken: new SecretString('refresh-token') };
  const provider = new JiraClient({ endpoint: `http://127.0.0.1:${address.port}`, cloudId: '11223344-a1b2-4b33-8c44-def123456789' }, { resolve: async () => credentials, resolveWithRefresh: async () => credentials });
  const flow = new GetJiraIssueFlow(newConnection(provider, { provider: 'atlassian', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `jira-get-${randomUUID()}`;
    await client.startFlow(flow, successId, { issueIdOrKey: 'OPS-42' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<Issue>().key).toBe('OPS-42');
    const rejectedId = `jira-get-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { issueIdOrKey: 'OPS-403' });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
