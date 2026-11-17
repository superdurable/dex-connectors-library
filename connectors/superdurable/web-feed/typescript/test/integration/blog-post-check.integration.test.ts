// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { afterAll, beforeAll, expect, it } from 'vitest';
import { BlogPostCheck, ConnectionName, SourceFeed, SourceSitemap, type CheckReport, type CheckRequest } from '../../examples/blog-post-check/flow.js';
import { Mock, readFeedRetry, readFeedUnchanged, readPageMetadataRead, readPageMetadataUnpublished, discoverFeedsDiscovered, readSitemapRead } from '../../src/mock.js';

// One Worker serves every test; the mock answers each call in order, so tests run one at a time.
const mock = new Mock(ConnectionName);
const flow = new BlogPostCheck(mock.connection());
let directory = '';
let cache: ReturnType<typeof openBlobCache>;
let worker: Worker;
let client: Client;

beforeAll(async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  directory = await mkdtemp(join(tmpdir(), 'dex-web-feed-ts-integration-'));
  cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const registry = new Registry([flow]);
  worker = new Worker(registry, cache, { serverAddress: dexAddress });
  client = new Client(registry, cache, { serverAddress: dexAddress });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000 });
}, 60_000);

afterAll(async () => {
  await client?.close(); await worker?.close(); cache?.close();
  if (directory) await rm(directory, { recursive: true, force: true });
});

async function runCheck(request: CheckRequest): Promise<{ status: string; report?: CheckReport; error?: string }> {
  const flowId = `blog-post-check-ts-${randomUUID()}`;
  await client.startFlow(flow, flowId, request);
  const result = await client.waitForFlow(flowId, 60_000);
  return { status: result.status, error: result.errorMessage, ...(result.status === 'completed' ? { report: result.singleOutput<CheckReport>() } : {}) };
}

it('reads the advertised feed through real Dex', async () => {
  const before = mock.readFeed().calls().length;
  const result = await runCheck({ siteUrl: 'https://blog.example.com/', publishedAfter: '2026-11-05T00:00:00Z' });
  expect(result.status, result.error).toBe('completed');
  expect(result.report).toMatchObject({ source: SourceFeed, sourceUrl: 'https://blog.example.com/feed.xml', etag: '"6f1e-5c2a"' });
  expect(result.report?.posts.map(post => post.url)).toEqual(['https://blog.example.com/posts/faster-builds']);
  expect(mock.readFeed().calls()[before]?.input.url).toBe('https://blog.example.com/feed.xml');
}, 90_000);

it('reads the first post text through real Dex', async () => {
  const before = mock.readPage().calls().length;
  const result = await runCheck({ feedUrl: 'https://blog.example.com/feed.xml', includeFirstPostText: true });
  expect(result.status, result.error).toBe('completed');
  expect(result.report?.posts).toHaveLength(2);
  expect(result.report?.firstPostText?.startsWith('Spring release notes\n\n')).toBe(true);
  const calls = mock.readPage().calls();
  expect(calls).toHaveLength(before + 1);
  expect(calls.at(-1)?.input).toEqual({ url: 'https://blog.example.com/posts/faster-builds', maxTextCharacters: 2000 });
  const withoutText = await runCheck({ feedUrl: 'https://blog.example.com/feed.xml' });
  expect(withoutText.report?.firstPostText).toBeUndefined();
  expect(mock.readPage().calls()).toHaveLength(before + 1);
}, 90_000);

it('completes an unchanged feed through the wired notModified branch', async () => {
  mock.readFeed().respond(readFeedUnchanged());
  const result = await runCheck({ feedUrl: 'https://blog.example.com/feed.xml', etag: '"6f1e-5c2a"' });
  expect(result.status, result.error).toBe('completed');
  expect(result.report).toMatchObject({ isUnchanged: true, posts: [] });
  expect(mock.readFeed().calls().at(-1)?.input.etag).toBe('"6f1e-5c2a"');
}, 90_000);

it('falls back to a sitemap index, its children, and the post pages', async () => {
  mock.discoverFeeds().respond(discoverFeedsDiscovered({ finalUrl: 'https://www.example.com/blog/', feeds: [], isTruncated: false, fetchedAt: '2026-11-10T10:00:00Z' }));
  const sitemap = { totalEntryCount: 1, matchedEntryCount: 1, hasMore: false, fetchedAt: '2026-11-10T10:00:00Z' };
  mock.readSitemap().respond(
    readSitemapRead({ ...sitemap, kind: 'sitemapIndex', finalUrl: 'https://www.example.com/sitemap.xml', entries: [], sitemaps: [{ url: 'https://www.example.com/sitemap-posts.xml' }, { url: 'https://www.example.com/sitemap-pages.xml' }] }),
    readSitemapRead({ ...sitemap, kind: 'urlset', finalUrl: 'https://www.example.com/sitemap-posts.xml', sitemaps: [], entries: [{ url: 'https://www.example.com/blog/older', lastModifiedAt: '2026-11-02T08:15:00Z' }] }),
    readSitemapRead({ ...sitemap, kind: 'urlset', finalUrl: 'https://www.example.com/sitemap-pages.xml', sitemaps: [], entries: [{ url: 'https://www.example.com/blog/newest', lastModifiedAt: '2026-11-09T18:00:00Z' }] }),
  );
  mock.readPageMetadata().respond(
    readPageMetadataRead({ finalUrl: 'https://www.example.com/blog/newest', title: 'Newest post', openGraph: {}, structuredData: {}, publishedAt: '2026-11-09T18:00:00Z', isTruncated: false, fetchedAt: '2026-11-10T10:00:00Z' }),
    readPageMetadataUnpublished(),
  );
  const result = await runCheck({ siteUrl: 'https://www.example.com/blog/', pathPrefix: '/blog/', publishedAfter: '2026-11-01T00:00:00Z', maxPosts: 2 });
  expect(result.status, result.error).toBe('completed');
  expect(result.report).toEqual({ source: SourceSitemap, sourceUrl: 'https://www.example.com/sitemap.xml', isUnchanged: false,
    posts: [{ url: 'https://www.example.com/blog/newest', title: 'Newest post', publishedAt: '2026-11-09T18:00:00Z' }] });
  const sitemapCalls = mock.readSitemap().calls().slice(-3);
  expect(sitemapCalls[0]?.input).toEqual({ url: 'https://www.example.com/sitemap.xml', pathPrefix: '/blog/', lastModifiedAfter: '2026-11-01T00:00:00Z', limit: 2 });
}, 90_000);

it('retries a transient feed failure through Dex', async () => {
  const before = mock.readFeed().calls().length;
  mock.readFeed().respond(readFeedRetry({ kind: 'AVAILABILITY', provider: 'web-feed', operation: 'readFeed', message: 'the site answered HTTP 503' }, 1000));
  const result = await runCheck({ feedUrl: 'https://blog.example.com/feed.xml' });
  expect(result.status, result.error).toBe('completed');
  expect(mock.readFeed().calls().length - before).toBe(2);
}, 90_000);

it('fails before any connector call on an invalid request', async () => {
  const before = mock.discoverFeeds().calls().length;
  const result = await runCheck({ siteUrl: 'https://blog.example.com/', maxPosts: 50 });
  expect(result.status).toBe('failed');
  expect(result.error).toContain('maxPosts');
  expect(mock.discoverFeeds().calls().length).toBe(before);
}, 90_000);
