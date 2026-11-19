// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { GeneratedMock, searchStoriesRetry, searchStoriesSearched } from '../../src/hackernewsmock.js';
import type { StorySearchHit, StorySearchPage } from '../../src/index.js';
import { ConnectionName, StorySearchFlow, type StorySearchReport, type StorySearchRequest } from './flow.js';

const story = (id: number, title: string, createdAt: string, extra: Partial<StorySearchHit> = {}): StorySearchHit => ({
  id, title, discussionUrl: `https://news.ycombinator.com/item?id=${id}`, author: 'writer', points: 5, commentCount: 1, createdAt, tags: ['story'], ...extra,
});
const release = story(42001, 'A compiler release', '2026-10-26T11:00:00Z', { url: 'https://example.com/compiler', tags: ['story', 'front_page'] });
// Three newest-first pages; story 42001 moves to the second page when a newer story arrives.
const pages: StorySearchPage[] = [
  { stories: [story(42003, 'Show HN: A compiler explorer', '2026-10-26T11:50:00Z', { url: 'https://example.com/explorer', tags: ['story', 'show_hn'] }), release], page: 0, pages: 3, totalHits: 5, hitsPerPage: 50, nextPage: 1 },
  { stories: [release, story(41990, 'Ask HN: How do you test a compiler?', '2026-10-26T09:30:00Z', { tags: ['story', 'ask_hn'], storyText: 'We fuzz the parser.' })], page: 1, pages: 3, totalHits: 5, hitsPerPage: 50, nextPage: 2 },
  { stories: [story(41980, 'Notes on compiler passes', '2026-10-26T08:15:00Z', { url: 'https://example.com/passes', points: 1 })], page: 2, pages: 3, totalHits: 5, hitsPerPage: 50, nextPage: 0 },
];

/** Exercise the checked-in Flow through a real Dex Worker and Client. */
export async function verifyStorySearchFlow(serverAddress: string): Promise<void> {
  const mock = new GeneratedMock(ConnectionName);
  const flow = new StorySearchFlow(mock.connection());
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-hackernews-search-example-'));
  const cache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  const scriptPages = (flowId: string) => {
    for (const [index, page] of pages.entries()) mock.searchStories().forFlow(flowId).when(input => (input.page ?? 0) === index, searchStoriesSearched(page));
  };
  const runSearch = async (flowId: string, request: StorySearchRequest) => {
    await client.startFlow(flow, flowId, request);
    return client.waitForFlow(flowId, 30_000);
  };
  const callsOf = (flowId: string) => mock.searchStories().calls().filter(call => call.flowId === flowId);
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });

    const allPagesId = `hackernews-search-${randomUUID()}`;
    scriptPages(allPagesId);
    const startedAt = Date.now();
    const allPages = await runSearch(allPagesId, { query: 'compiler' });
    assert.equal(allPages.status, 'completed', allPages.errorMessage);
    const report = allPages.singleOutput<StorySearchReport>();
    assert.deepEqual(report.stories.map(entry => entry.id), [42003, 42001, 41990, 41980]);
    assert.equal(report.pagesRead, 3);
    assert.equal(report.hasUnreadPages, false);
    assert.equal(report.stories[2]?.url, undefined, 'the Ask HN text post has no source URL');
    assert.ok(Math.abs(Date.parse(report.createdAfter) - (startedAt - 24 * 3600_000)) < 60_000);
    assert.deepEqual(callsOf(allPagesId).map(call => call.input), [0, 1, 2].map(page => ({
      query: 'compiler', sortBy: 'date', createdAfter: report.createdAfter, page, hitsPerPage: 50,
    })));

    const budgetId = `hackernews-search-budget-${randomUUID()}`;
    scriptPages(budgetId);
    const budget = await runSearch(budgetId, { query: 'compiler', maxPages: 2, tags: ['show_hn'], minPoints: 2, sortBy: 'relevance' });
    assert.equal(budget.status, 'completed', budget.errorMessage);
    const budgetReport = budget.singleOutput<StorySearchReport>();
    assert.deepEqual(budgetReport.stories.map(entry => entry.id), [42003, 42001, 41990]);
    assert.equal(budgetReport.pagesRead, 2);
    assert.equal(budgetReport.hasUnreadPages, true);
    assert.deepEqual(callsOf(budgetId)[0]?.input, {
      query: 'compiler', sortBy: 'relevance', createdAfter: budgetReport.createdAfter, page: 0, hitsPerPage: 50, tags: ['show_hn'], minPoints: 2,
    });

    const retryId = `hackernews-search-retry-${randomUUID()}`;
    mock.searchStories().forFlow(retryId).respond(
      searchStoriesRetry({ kind: 'RATE_LIMIT', provider: 'hacker-news', operation: 'searchStories', message: 'rate limited' }, 10),
      searchStoriesSearched(pages[2]!),
    );
    const retried = await runSearch(retryId, { query: 'compiler' });
    assert.equal(retried.status, 'completed', retried.errorMessage);
    assert.deepEqual(callsOf(retryId).map(call => call.retry), [true, undefined]);

    const invalidId = `hackernews-search-invalid-${randomUUID()}`;
    const invalid = await runSearch(invalidId, { query: ' ' });
    assert.equal(invalid.status, 'failed');
    assert.equal(callsOf(invalidId).length, 0);
    mock.assertNoFailures();
  } finally { await client.close(); await worker.close(); cache.close(); await rm(directory, { recursive: true, force: true }); }
}

if (process.argv[1]?.endsWith('/integration.ts')) {
  verifyStorySearchFlow(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => { console.error(error); process.exitCode = 1; });
}
