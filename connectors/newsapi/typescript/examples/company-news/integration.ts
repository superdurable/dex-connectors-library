// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { Mock, searchArticlesInvalidKey, searchArticlesRetry, searchArticlesSearchedDefaultAnswer } from '../../src/newsapimock.js';
import { CompanyNewsFlow } from './flow.js';
import { ConnectionName, type CompanyNewsOutcome } from './model.js';

/** Exercise the checked-in Flow through a real Dex Worker, Client, and retry boundary. */
export async function verifyCompanyNewsFlow(serverAddress: string): Promise<void> {
  const mock = new Mock(ConnectionName);
  const flow = new CompanyNewsFlow(mock.connection);
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-newsapi-integration-'));
  const blobCache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const worker = new Worker(registry, blobCache, { serverAddress });
  const client = new Client(registry, blobCache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });

    const successId = `newsapi-success-${randomUUID()}`;
    await client.startFlow(flow, successId, { company: 'Meridian', days: 7, maxArticles: 2 });
    const success = await client.waitForFlow(successId, 30_000);
    assert.equal(success.status, 'completed', success.errorMessage);
    const outcome = success.singleOutput<CompanyNewsOutcome>();
    assert.equal(outcome.status, 'found');
    assert.equal(outcome.articles.length, 2);
    assert.equal(mock.searchArticles.calls()[0]?.input.query, 'Meridian');
    assert.deepEqual(mock.searchArticles.calls()[0]?.input.searchIn, ['title']);

    const retryId = `newsapi-retry-${randomUUID()}`;
    mock.searchArticles.forFlow(retryId).respond(
      searchArticlesRetry({ kind: 'RATE_LIMIT', provider: 'newsapi', operation: 'searchArticles', message: 'provider rate limited search' }, 10),
      searchArticlesSearchedDefaultAnswer(),
    );
    await client.startFlow(flow, retryId, { company: 'Meridian' });
    const retried = await client.waitForFlow(retryId, 30_000);
    assert.equal(retried.status, 'completed', retried.errorMessage);
    assert.deepEqual(mock.searchArticles.calls().filter(call => call.flowId === retryId).map(call => call.retry), [true, undefined]);

    const rejectedId = `newsapi-rejected-${randomUUID()}`;
    mock.searchArticles.forFlow(rejectedId).respond(searchArticlesInvalidKey());
    await client.startFlow(flow, rejectedId, { company: 'Meridian' });
    const rejected = await client.waitForFlow(rejectedId, 30_000);
    assert.equal(rejected.status, 'failed');
    assert.match(rejected.errorMessage ?? '', /providerRejected/);
    assert.match(rejected.errorMessage ?? '', /AUTHENTICATION/);
    assert.equal(mock.searchArticles.calls().find(call => call.flowId === rejectedId)?.branch, 'providerRejected');

    mock.searchArticles.assertNoFailures();
  } finally {
    await client.close();
    await worker.close();
    blobCache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[1]?.endsWith('/integration.ts')) {
  verifyCompanyNewsFlow(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => {
    console.error(error);
    process.exitCode = 1;
  });
}
