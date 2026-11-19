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
import { TMDBClient, newConnection, type MoviePage } from '../src/index.js';
import { ConnectionName, MovieSearchFlow } from '../examples/movie-search/flow.js';

it('searches through a real Dex Worker and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const provider = createServer((request, response) => {
    requests.push(request.url ?? '');
    if (request.headers.authorization !== 'Bearer test-secret-token') { response.writeHead(401).end(); return; }
    if (request.url?.includes('REJECT')) { response.writeHead(403).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ page: 1, total_pages: 1, total_results: 1,
      results: [{ id: 343611, title: 'Jack Reacher', overview: 'A thriller.', release_date: '2016-10-19' }] }));
  });
  provider.listen(0, '127.0.0.1');
  await once(provider, 'listening');
  const address = provider.address();
  if (!address || typeof address === 'string') throw new Error('TMDB fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-tmdb-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const tmdb = new TMDBClient({ baseUrl: `http://127.0.0.1:${address.port}/3` }, {
    resolve: async () => ({ apiKey: new SecretString('test-secret-token') }),
  });
  const flow = new MovieSearchFlow(newConnection(tmdb, { provider: 'tmdb', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `tmdb-search-${randomUUID()}`;
    await client.startFlow(flow, successId, { query: 'Jack Reacher', page: 1 });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<MoviePage>().results?.[0]?.id).toBe(343611);
    const rejectedId = `tmdb-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { query: 'REJECT', page: 1 });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => provider.close(() => resolve()));
  }
}, 90_000);
