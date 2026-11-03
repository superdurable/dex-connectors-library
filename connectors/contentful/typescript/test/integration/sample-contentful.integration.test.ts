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
import { ContentfulClient } from '../../src/client.js';
import { newConnection, type GetEntryOutput, type ListEntriesOutput } from '../../src/index.js';
import { ConnectionName, GetEntryFlow, ListEntriesFlow } from '../../examples/sample-contentful/flow.js';

it('reads both Contentful operations through real Dex, retries, and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const requests: string[] = [];
  let entryAttempts = 0;
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer test-token') { response.writeHead(401).end(); return; }
    if (request.url === '/spaces/space123/environments/master/entries/entry-meridian') {
      entryAttempts++;
      if (entryAttempts === 1) { response.writeHead(429).end(); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ sys: { id: 'entry-meridian' }, fields: { title: 'Meridian' } }));
      return;
    }
    if (request.url === '/spaces/space123/environments/master/entries?limit=2&skip=1') {
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ items: [{ sys: { id: 'entry-meridian' } }], total: 3, limit: 2, skip: 1 }));
      return;
    }
    response.writeHead(403).end('{"message":"private sentinel"}');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Contentful fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-contentful-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new ContentfulClient({ baseUrl: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ deliveryToken: new SecretString('test-token') }) });
  const connection = newConnection(provider, { provider: 'contentful', name: ConnectionName });
  const getEntry = new GetEntryFlow(connection);
  const listEntries = new ListEntriesFlow(connection);
  const registry = new Registry([getEntry, listEntries]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const entryId = `contentful-entry-${randomUUID()}`;
    await client.startFlow(getEntry, entryId, { spaceId: 'space123', environmentId: 'master', entryId: 'entry-meridian' });
    const found = await client.waitForFlow(entryId, 60_000);
    expect(found.status, found.errorMessage).toBe('completed');
    expect((found.singleOutput<GetEntryOutput>().data as { sys: { id: string } }).sys.id).toBe('entry-meridian');
    expect(entryAttempts).toBe(2);

    const listId = `contentful-list-${randomUUID()}`;
    await client.startFlow(listEntries, listId, { spaceId: 'space123', environmentId: 'master', limit: 2, skip: 1 });
    const listed = await client.waitForFlow(listId, 60_000);
    expect(listed.status, listed.errorMessage).toBe('completed');
    expect((listed.singleOutput<ListEntriesOutput>().data as { total: number }).total).toBe(3);

    const rejectionId = `contentful-rejected-${randomUUID()}`;
    await client.startFlow(getEntry, rejectionId, { spaceId: 'space123', environmentId: 'master', entryId: 'rejected' });
    const rejected = await client.waitForFlow(rejectionId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(rejected.errorMessage).not.toContain('private sentinel');
    expect(requests).toHaveLength(4);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
