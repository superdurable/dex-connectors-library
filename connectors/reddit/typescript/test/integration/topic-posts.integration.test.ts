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
import { RedditClient } from '../../src/client.js';
import { newConnection, type Connection, type ListedPost, type PostPage } from '../../src/index.js';
import { Mock, listSubredditPostsListed, listSubredditPostsSubredditMissing, searchPostsSearched } from '../../src/mock.js';
import { ConnectionName, TopicPostsFlow, type TopicPostsOutput } from '../../examples/topic-posts/flow.js';

// Reddit's JSON for one newest page of r/golang: a pinned thread to filter out and a post the search repeats.
const fakeListing = { kind: 'Listing', data: { after: 't3_1a0002', dist: 2, children: [
  { kind: 't3', data: { name: 't3_1a0001', title: 'Weekly questions thread', subreddit: 'golang', author: 'example_moderator', permalink: '/r/golang/comments/1a0001/weekly_questions_thread/', url: 'https://www.reddit.com/r/golang/comments/1a0001/weekly_questions_thread/', is_self: true, selftext: 'Generics questions welcome.', score: 12, num_comments: 40, created_utc: 1794211200.0, over_18: false, spoiler: false, stickied: true } },
  { kind: 't3', data: { name: 't3_1a0002', title: 'Generics and iterators together', subreddit: 'golang', author: 'example_author_two', permalink: '/r/golang/comments/1a0002/generics_and_iterators_together/', url: 'https://example.com/iterators', is_self: false, selftext: '', score: 31, num_comments: 4, created_utc: 1794475700.0, over_18: false, spoiler: false, stickied: false } },
], before: null } };
const fakeSearch = { kind: 'Listing', data: { after: null, dist: 2, children: [
  { kind: 't3', data: { name: 't3_1a0004', title: 'Benchmarking generic containers', subreddit: 'golang', author: 'example_author_five', permalink: '/r/golang/comments/1a0004/benchmarking_generic_containers/', url: 'https://www.reddit.com/r/golang/comments/1a0004/benchmarking_generic_containers/', is_self: true, selftext: 'Results for **maps** & slices.', score: 44, num_comments: 15, created_utc: 1794564000.0, over_18: false, spoiler: false, stickied: false } },
  fakeListing.data.children[1],
], before: null } };

it('waits out a rate-limit reset and combines a fake Reddit listing and search through real Dex', async () => {
  const requests: URL[] = [];
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer synthetic-reddit-token' || request.headers['user-agent'] !== 'desktop:synthetic-client:v0.1.0 (by /u/example_user)') {
      response.writeHead(401).end(); return;
    }
    const requestURL = new URL(request.url ?? '', 'http://127.0.0.1');
    requests.push(requestURL);
    // The first listing request is rate limited with only Reddit's reset header, which sets the retry delay.
    if (requests.length === 1) { response.writeHead(429, { 'X-Ratelimit-Remaining': '0.0', 'X-Ratelimit-Reset': '1' }).end(); return; }
    const body = requestURL.pathname === '/r/golang/new' ? fakeListing : requestURL.pathname === '/r/golang/search' ? fakeSearch : undefined;
    if (!body) { response.writeHead(404).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json', 'X-Ratelimit-Remaining': '98.0' }).end(JSON.stringify(body));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Reddit fixture port unavailable');
  try {
    const provider = new RedditClient({ redditUsername: 'example_user', baseUrl: `http://127.0.0.1:${address.port}` }, {
      resolve: async () => ({ oauthClientId: 'synthetic-client', oauthClientSecret: new SecretString('synthetic-secret'), accessToken: new SecretString('synthetic-reddit-token'), refreshToken: new SecretString('synthetic-refresh') }),
    });
    const startedAt = Date.now();
    const result = await runTopicPostsFlow(newConnection(provider, { provider: 'reddit', name: ConnectionName }), 'golang');
    expect(result.status, result.errorMessage).toBe('completed');
    expect(Date.now() - startedAt).toBeGreaterThanOrEqual(1_000);
    const output = result.singleOutput<TopicPostsOutput>();
    expect(output.posts.map(post => post.id)).toEqual(['t3_1a0004', 't3_1a0002']);
    expect(output.posts[0]).toMatchObject({ permalink: 'https://www.reddit.com/r/golang/comments/1a0004/benchmarking_generic_containers/', selfText: 'Results for **maps** & slices.', createdAt: '2026-11-13T10:00:00Z' });
    expect(output.posts[1]?.linkUrl).toBe('https://example.com/iterators');
    expect(output.searchAfter).toBeUndefined();
    expect(requests.map(url => url.pathname)).toEqual(['/r/golang/new', '/r/golang/new', '/r/golang/search']);
    expect(Object.fromEntries(requests[2]?.searchParams ?? [])).toEqual({
      limit: '25', q: 'generics', raw_json: '1', restrict_sr: 'true', sort: 'new', t: 'week', type: 'link',
    });
  } finally {
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);

it('combines scripted mock pages through real Dex', async () => {
  const mock = new Mock(ConnectionName);
  mock.listSubredditPosts().respond(listSubredditPostsListed(page(
    mockPost('t3_1a0001', 'Weekly questions thread', '2026-11-09T08:00:00Z', { isStickied: true, selfText: 'Ask small questions here.' }),
    mockPost('t3_1a0003', 'How do generics interact with interfaces?', '2026-11-12T16:30:00Z'),
    mockPost('t3_1a0002', 'A walkthrough of the new release notes', '2026-11-12T09:15:00Z'),
  )));
  mock.searchPosts().respond(searchPostsSearched({ ...page(
    mockPost('t3_1a0004', 'Generics performance compared with interfaces', '2026-11-13T10:00:00Z'),
    mockPost('t3_1a0003', 'How do generics interact with interfaces?', '2026-11-12T16:30:00Z'),
  ), after: 't3_1a0003' }));
  const result = await runTopicPostsFlow(mock.connection(), 'golang');
  expect(result.status, result.errorMessage).toBe('completed');
  const output = result.singleOutput<TopicPostsOutput>();
  expect(output.posts.map(post => post.id)).toEqual(['t3_1a0004', 't3_1a0003']);
  expect(output.searchAfter).toBe('t3_1a0003');
  expect(mock.listSubredditPosts().calls().map(call => call.input)).toEqual([{ subreddit: 'golang', sort: 'new' }]);
  expect(mock.searchPosts().calls().map(call => call.input)).toEqual([{ query: 'generics', subreddit: 'golang', sort: 'new', timeWindow: 'week' }]);
  mock.assertNoFailures();
}, 120_000);

it('fails the Flow when the subreddit is unavailable', async () => {
  const mock = new Mock(ConnectionName);
  mock.listSubredditPosts().respond(listSubredditPostsSubredditMissing());
  const result = await runTopicPostsFlow(mock.connection(), 'nosuchcommunity');
  // The unwired optional branch fails the Flow, so a misspelled subreddit stays visible instead of reading as no posts.
  expect(result.status).toBe('failed');
  expect(mock.listSubredditPosts().calls()).toHaveLength(1);
  expect(mock.searchPosts().calls()).toHaveLength(0);
}, 120_000);

async function runTopicPostsFlow(connection: Connection, subreddit: string) {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const directory = await mkdtemp(join(tmpdir(), 'dex-reddit-ts-topic-posts-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const flow = new TopicPostsFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `reddit-typescript-topic-posts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { subreddit, topic: 'generics' });
    return await client.waitForFlow(flowId, 60_000);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

function mockPost(id: string, title: string, createdAt: string, overrides: Partial<ListedPost> = {}): ListedPost {
  return {
    id, title, subreddit: 'golang', author: 'example_author', permalink: `https://www.reddit.com/r/golang/comments/${id.slice(3)}/`,
    isSelf: true, score: 10, commentCount: 2, createdAt, isOver18: false, isSpoiler: false, isStickied: false, ...overrides,
  };
}

function page(...posts: ListedPost[]): PostPage { return { posts }; }
