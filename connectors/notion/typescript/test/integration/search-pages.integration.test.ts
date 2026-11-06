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
import { NotionClient } from '../../src/client.js';
import { newConnection, type SearchOutput } from '../../src/index.js';
import { ConnectionName, SearchPagesFlow } from '../../examples/search-pages/flow.js';

it('searches through a real Dex Worker, retries, and fails an unwired optional branch', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer(async (request, response) => {
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { query?: string };
    if (request.method !== 'POST' || request.url !== '/v1/search' || request.headers.authorization !== 'Bearer ntn_test_token_value' || request.headers['notion-version'] !== '2026-03-11') { response.writeHead(400).end(); return; }
    if (body.query === 'missing') { response.writeHead(200).end(JSON.stringify({ results: [], next_cursor: null, has_more: false })); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(429).end(JSON.stringify({ object: 'error', status: 429, code: 'rate_limited' })); return; }
    response.writeHead(200).end(JSON.stringify({ results: [{ object: 'data_source', id: '2a4d6f80-1b3c-4d5e-8f70-112233445566', title: [{ plain_text: 'Form submissions' }], parent: { type: 'database_id', database_id: '3b5e7091-2c4d-4e6f-9081-223344556677' } }], next_cursor: null, has_more: false }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Notion provider fixture has no port');
  const directory = await mkdtemp(join(tmpdir(), 'dex-notion-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new NotionClient({ endpoint: `http://127.0.0.1:${address.port}` }, { resolve: async () => ({ apiToken: new SecretString('ntn_test_token_value') }) });
  const connection = newConnection(provider, { provider: 'notion', name: ConnectionName });
  const flow = new SearchPagesFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const foundId = `notion-search-${randomUUID()}`;
    await client.startFlow(flow, foundId, { query: 'Form' });
    const found = await client.waitForFlow(foundId, 60_000);
    expect(found.status, found.errorMessage).toBe('completed');
    expect(found.singleOutput<SearchOutput>().matches?.[0]?.title).toBe('Form submissions');
    expect(attempts).toBe(2);
    const missingId = `notion-search-${randomUUID()}`;
    await client.startFlow(flow, missingId, { query: 'missing' });
    const missing = await client.waitForFlow(missingId, 60_000);
    expect(missing.status).toBe('failed');
    expect(missing.errorMessage).toContain('noMatch');
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
