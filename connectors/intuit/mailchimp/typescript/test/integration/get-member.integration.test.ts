// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { MailchimpClient } from '../../src/client.js';
import { newConnection, type Member } from '../../src/index.js';
import { ConnectionName, GetMemberFlow } from '../../examples/get-member/flow/workflow.js';

it('reads a contact through real Dex and fails an unwired notFound branch', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer TESTKEY1-us6') { response.writeHead(401).end(); return; }
    const hash = request.url?.split('/').at(-1);
    if (hash === createHash('md5').update('missing@example.com').digest('hex')) { response.writeHead(404, { 'Content-Type': 'application/json' }).end('{"title":"Resource Not Found"}'); return; }
    const body = { id: hash, email_address: 'ben@example.com', status: 'subscribed', tags_count: 1, tags: [{ name: 'spring-launch' }], list_id: '57afe96172' };
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify(body));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Mailchimp fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-mailchimp-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new MailchimpClient({}, { resolve: async () => ({ apiKey: new SecretString('TESTKEY1-us6') }) },
    { apiBaseURL: `http://127.0.0.1:${address.port}/3.0` });
  const flow = new GetMemberFlow(newConnection(provider, { provider: 'mailchimp', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successID = `mailchimp-read-${randomUUID()}`;
    await client.startFlow(flow, successID, { listId: '57afe96172', emailAddress: 'ben@example.com' });
    const success = await client.waitForFlow(successID, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<Member>().status).toBe('subscribed');
    const missingID = `mailchimp-missing-${randomUUID()}`;
    await client.startFlow(flow, missingID, { listId: '57afe96172', emailAddress: 'missing@example.com' });
    const missing = await client.waitForFlow(missingID, 60_000);
    expect(missing.status).toBe('failed');
    expect(missing.errorMessage).toContain('notFound');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
