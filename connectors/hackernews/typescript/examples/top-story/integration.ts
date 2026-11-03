// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { GeneratedMock, getItemCompilerStory, listStoryIDsRetry, listStoryIDsDeveloperFeed } from '../../src/hackernewsmock.js';
import { ConnectionName, TopStoryFlow, type TopStoryOutcome } from './flow.js';

/** Exercise the checked-in Flow through a real Dex Worker and Client. */
export async function verifyTopStoryFlow(serverAddress: string): Promise<void> {
  const mock = new GeneratedMock(ConnectionName);
  const flow = new TopStoryFlow(mock.connection());
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-hackernews-example-'));
  const cache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });
    const successId = `hackernews-success-${randomUUID()}`;
    await client.startFlow(flow, successId, { feed: 'top' });
    const success = await client.waitForFlow(successId, 30_000);
    assert.equal(success.status, 'completed', success.errorMessage);
    assert.deepEqual(success.singleOutput<TopStoryOutcome>(), { id: 42001, title: 'A compiler release', sourceUrl: 'https://example.com/compiler', discussionUrl: 'https://news.ycombinator.com/item?id=42001' });
    assert.equal(mock.listStoryIDs().calls()[0]?.input.limit, 1);

    const retryId = `hackernews-retry-${randomUUID()}`;
    mock.listStoryIDs().forFlow(retryId).respond(
      listStoryIDsRetry({ kind: 'RATE_LIMIT', provider: 'hacker-news', operation: 'listStoryIDs', message: 'rate limited' }, 10),
      listStoryIDsDeveloperFeed(),
    );
    mock.getItem().forFlow(retryId).respond(getItemCompilerStory());
    await client.startFlow(flow, retryId, { feed: 'top' });
    const retried = await client.waitForFlow(retryId, 30_000);
    assert.equal(retried.status, 'completed', retried.errorMessage);
    assert.deepEqual(mock.listStoryIDs().calls().filter(call => call.flowId === retryId).map(call => call.retry), [true, undefined]);
    mock.assertNoFailures();
  } finally { await client.close(); await worker.close(); cache.close(); await rm(directory, { recursive: true, force: true }); }
}

if (process.argv[1]?.endsWith('/integration.ts')) {
  verifyTopStoryFlow(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => { console.error(error); process.exitCode = 1; });
}
