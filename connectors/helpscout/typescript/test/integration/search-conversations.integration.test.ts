// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, StepList, Worker, gracefulComplete, openBlobCache, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider, type MutationResult } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { expect, it } from 'vitest';
import { HelpScoutClient } from '../../src/client.js';
import { newConnection, newReplyToConversationStep, type ConversationReply, type ReplyToConversationInput, type SearchConversationsOutput } from '../../src/index.js';
import { ConnectionName, SearchConversationsFlow } from '../../examples/search-conversations/flow/workflow.js';

it('searches through a real Dex Worker and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer inert-access-token') { response.writeHead(401).end(); return; }
    const url = new URL(request.url ?? '/', 'http://localhost');
    if (request.method !== 'GET' || url.pathname !== '/v2/conversations') { response.writeHead(404).end(); return; }
    if (url.searchParams.get('tag') === 'REJECT') { response.writeHead(403, { 'Content-Type': 'application/json' }).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ _embedded: { conversations: [{ id: 501, number: 12, subject: 'Billing question', status: 'active', mailboxId: 12345, threads: 1, tags: [{ tag: 'billing' }] }] }, page: { totalElements: 1, totalPages: 1 } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Help Scout fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-helpscout-search-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'helpscout', name: ConnectionName };
  const provider = new HelpScoutClient({}, new StaticCredentialProvider([[connection, {
    appId: 'inert-app-id', appSecret: new SecretString('inert-app-secret'), accessToken: new SecretString('inert-access-token'),
  }]]), { send: request => sendProviderRequest({ ...request, url: request.url.replace('https://api.helpscout.net', `http://127.0.0.1:${address.port}`), addressPolicy: 'privateNetworkHTTP' }) });
  const flow = new SearchConversationsFlow(newConnection(provider, connection));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `helpscout-search-${randomUUID()}`;
    await client.startFlow(flow, successId, { mailboxId: 12345, status: 'active', page: 1 });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<SearchConversationsOutput>().conversations?.[0]?.id).toBe(501);
    const rejectedId = `helpscout-search-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { mailboxId: 12345, status: 'active', page: 1, tag: 'REJECT' });
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

it('retries a fenced note only after Help Scout confirms non-application', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  let writes = 0;
  const server = createServer((request, response) => {
    if (request.method !== 'POST' || request.url !== '/v2/conversations/501/notes') { response.writeHead(404).end(); return; }
    writes++;
    if (writes === 1) response.writeHead(503).end('{}');
    else response.writeHead(201, { 'Resource-Id': '568' }).end();
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Help Scout fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-helpscout-reply-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'helpscout', name: ConnectionName };
  const provider = new HelpScoutClient({}, new StaticCredentialProvider([[connection, {
    appId: 'inert-app-id', appSecret: new SecretString('inert-app-secret'), accessToken: new SecretString('inert-access-token'),
  }]]), { send: request => sendProviderRequest({ ...request, url: request.url.replace('https://api.helpscout.net', `http://127.0.0.1:${address.port}`), addressPolicy: 'privateNetworkHTTP' }) });
  const bound = newConnection(provider, connection);
  class CompleteReply implements Step<MutationResult<ConversationReply>> {
    getStepType(): string { return 'CompleteHelpScoutReplyIntegration'; }
    execute(_context: Context, result: MutationResult<ConversationReply>): StepDecision { return gracefulComplete(result.value); }
  }
  class ReplyFlow implements Flow<ReplyToConversationInput> {
    readonly #complete = new CompleteReply();
    getFlowType(): string { return 'HelpScoutReplyIntegrationFlow'; }
    getSteps(): StepList<ReplyToConversationInput> {
      return StepList.startStep(newReplyToConversationStep<ReplyToConversationInput>({
        stepType: 'ReplyHelpScoutIntegration',
        annotations: { groupId: 'helpscout-reply', groupLabel: 'Help Scout Reply', explanation: 'Add one fenced Help Scout note.' },
        connection: bound, connectionName: ConnectionName, mapToOperationInput: input => input, replied: this.#complete,
      })).otherSteps(this.#complete);
    }
    getPersistenceSchema(): PersistenceSchema { return { attributes: [] }; }
  }
  const flow = new ReplyFlow();
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `helpscout-reply-${randomUUID()}`;
    await client.startFlow(flow, flowId, { conversationId: 501, text: 'Internal triage note', isInternalNote: true });
    const result = await client.waitForFlow(flowId, 90_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<ConversationReply>().threadId).toBe(568);
    expect(writes).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
