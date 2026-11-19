// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { RedditClient } from '../src/client.js';
import { MaxPostPageLimit, MaxSearchQueryCharacters, MaxSelfTextBytes, type ListSubredditPostsInput, type PostPage, type SearchPostsInput } from '../src/index.js';

const connection = { provider: 'reddit', name: 'reddit' };
const accessToken = 'synthetic-reddit-token';

type Responder = (request: ProviderRequest) => ProviderResponse | Promise<ProviderResponse>;

function newClient(respond: Responder, maxResponseBytes?: number) {
  const requests: ProviderRequest[] = [];
  const credentials = {
    oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret'),
    accessToken: new SecretString(accessToken), refreshToken: new SecretString('refresh-token'),
  };
  const client = new RedditClient({ baseUrl: 'http://127.0.0.1:12345', redditUsername: 'example', ...(maxResponseBytes ? { maxResponseBytes } : {}) },
    new StaticCredentialProvider([[connection, credentials]]), { send: async request => sendWithinLimit(requests, respond, request) });
  return { client, requests };
}

// Like the SDK transport, a body above the request's limit rejects with its status and headers.
async function sendWithinLimit(requests: ProviderRequest[], respond: Responder, request: ProviderRequest): Promise<ProviderResponse> {
  requests.push(request);
  const response = await respond(request);
  if (response.body.length > request.maxResponseBytes) throw new BodyTooLargeError(response.status, response.headers);
  return response;
}

function call(operationId: string): Call {
  return {
    context: { flowId: 'reddit-provider-test', cancellationSignal: new AbortController().signal } as Call['context'],
    id: 'reddit-provider-test-call', connection, operation: { connectorId: 'reddit', operationId },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
}

const listPosts = (client: RedditClient, input: ListSubredditPostsInput) => client.listSubredditPosts().invoke(call('listSubredditPosts'), input);
const searchPosts = (client: RedditClient, input: SearchPostsInput) => client.searchPosts().invoke(call('searchPosts'), input);

function json(status: number, body: unknown, headers: Record<string, string> = {}): ProviderResponse {
  return { status, headers, body: Buffer.from(typeof body === 'string' ? body : JSON.stringify(body)) };
}

/** One Reddit t3 child whose data overrides a valid post's fields. */
function post(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  const name = typeof overrides.name === 'string' ? overrides.name : 't3_abc123';
  return {
    kind: 't3', data: {
      name, title: 'Example title', subreddit: 'golang', author: 'example_author', permalink: `/r/golang/comments/${name.replace('t3_', '')}/example_title/`,
      url: '', is_self: false, selftext: '', score: 42, num_comments: 7, created_utc: 1794501000.0, over_18: false, spoiler: false, stickied: false,
      ...overrides,
    },
  };
}

function listing(after: string | null, ...children: unknown[]) {
  return { kind: 'Listing', data: { after, dist: children.length, children, before: null } };
}

function expectBranch(attempt: QueryAttempt<PostPage>, branch: string, failureKind?: string): PostPage {
  if (attempt.kind !== 'branch') throw new Error(`expected branch ${branch}, got ${attempt.kind}`);
  expect(attempt.branch).toBe(branch);
  expect(attempt.failure?.kind).toBe(failureKind);
  return attempt.value;
}

describe('listSubredditPosts', () => {
  it('sends a bounded listing request and projects posts', async () => {
    const longSelfText = '\u00e9'.repeat(MaxSelfTextBytes); // two bytes per character, so the cut lands mid-text
    const { client, requests } = newClient(() => json(200, listing('t3_next02',
      post({ name: 't3_self01', is_self: true, selftext: longSelfText, url: 'https://www.reddit.com/r/golang/comments/self01/a_question/', stickied: true, author_fullname: 't2_not_persisted' }),
      post({ name: 't3_link01', subreddit: 'programming', url: 'https://example.com/article', over_18: true, spoiler: true }),
      post({ name: 't3_cross1', url: '/r/golang/comments/orig01/original_post/' }),
      post({ name: 't3_odd001', url: 'javascript:alert(1)' }),
    )));
    const page = expectBranch(await listPosts(client, { subreddit: 'golang+programming', sort: 'top', timeWindow: 'week', limit: 50, after: 't3_prev01' }), 'listed');
    const url = new URL(requests[0]?.url ?? '');
    expect(url.pathname).toBe('/r/golang+programming/top');
    expect(Object.fromEntries(url.searchParams)).toEqual({ after: 't3_prev01', limit: '50', raw_json: '1', t: 'week' });
    expect(requests[0]?.headers).toMatchObject({ Authorization: `Bearer ${accessToken}`, 'User-Agent': 'desktop:client-id:v0.1.0 (by /u/example)' });
    expect(page.after).toBe('t3_next02');
    expect(page.posts).toHaveLength(4);
    const [selfPost, linkPost, crosspost, oddPost] = page.posts ?? [];
    expect(selfPost).toMatchObject({ id: 't3_self01', permalink: 'https://www.reddit.com/r/golang/comments/self01/example_title/', isSelf: true, isSelfTextTruncated: true, isStickied: true, createdAt: '2026-11-12T16:30:00Z' });
    expect(selfPost?.linkUrl).toBeUndefined();
    expect(selfPost?.selfText).toBe('\u00e9'.repeat(MaxSelfTextBytes / 2));
    expect(linkPost).toEqual({
      id: 't3_link01', title: 'Example title', subreddit: 'programming', author: 'example_author',
      permalink: 'https://www.reddit.com/r/golang/comments/link01/example_title/', linkUrl: 'https://example.com/article', isSelf: false,
      score: 42, commentCount: 7, createdAt: '2026-11-12T16:30:00Z', isOver18: true, isSpoiler: true, isStickied: false,
    });
    expect(crosspost?.linkUrl).toBe('https://www.reddit.com/r/golang/comments/orig01/original_post/');
    expect(oddPost?.linkUrl).toBeUndefined();
    expect(JSON.stringify(page)).not.toContain('not_persisted');
  });

  it.each([
    ['blank sort reads new', { subreddit: 'golang' }, '/r/golang/new', { limit: '25', raw_json: '1' }],
    ['top without a window reads the last day', { subreddit: 'golang', sort: 'top' }, '/r/golang/top', { limit: '25', raw_json: '1', t: 'day' }],
    ['hot', { subreddit: 'Go_Lang', sort: 'hot', limit: 100 }, '/r/Go_Lang/hot', { limit: '100', raw_json: '1' }],
    ['rising', { subreddit: 'de', sort: 'rising', limit: 1 }, '/r/de/rising', { limit: '1', raw_json: '1' }],
  ] as const)('%s', async (_name, input, path, query) => {
    const { client, requests } = newClient(() => json(200, listing(null)));
    expect(expectBranch(await listPosts(client, input), 'listed')).toEqual({ posts: [] });
    const url = new URL(requests[0]?.url ?? '');
    expect(url.pathname).toBe(path);
    expect(Object.fromEntries(url.searchParams)).toEqual(query);
  });

  it('follows after until the listing ends', async () => {
    const { client, requests } = newClient(request => {
      const after = new URL(request.url).searchParams.get('after') ?? '';
      if (after === '') return json(200, listing('t3_page01', post({ name: 't3_page01' })));
      if (after === 't3_page01') return json(200, listing('t3_page02', post({ name: 't3_page01' }), post({ name: 't3_page02' })));
      return json(200, listing(null, post({ name: 't3_page03' })));
    });
    const ids: string[] = [];
    let after = '';
    for (let page = 0; page < 5; page++) {
      const value = expectBranch(await listPosts(client, { subreddit: 'golang', ...(after ? { after } : {}) }), 'listed');
      for (const item of value.posts ?? []) if (!ids.includes(item.id)) ids.push(item.id);
      after = value.after ?? '';
      if (!after) break;
    }
    expect(requests.map(request => new URL(request.url).searchParams.get('after') ?? '')).toEqual(['', 't3_page01', 't3_page02']);
    expect(ids).toEqual(['t3_page01', 't3_page02', 't3_page03']);
  });

  it.each([
    ['blank subreddit', { subreddit: '' }], ['r/ prefix', { subreddit: 'r/golang' }], ['leading underscore', { subreddit: '_golang' }],
    ['one character', { subreddit: 'g' }], ['22 characters', { subreddit: 'g'.repeat(22) }], ['space', { subreddit: 'go lang' }],
    ['empty name in multi', { subreddit: 'golang+' }], ['eleven names', { subreddit: Array(11).fill('golang').join('+') }],
    ['unknown sort', { subreddit: 'golang', sort: 'controversial' }],
    ['time window without top', { subreddit: 'golang', sort: 'new', timeWindow: 'week' }],
    ['time window with blank sort', { subreddit: 'golang', timeWindow: 'day' }],
    ['unknown time window', { subreddit: 'golang', sort: 'top', timeWindow: 'decade' }],
    ['negative limit', { subreddit: 'golang', limit: -1 }], ["limit above Reddit's page size", { subreddit: 'golang', limit: MaxPostPageLimit + 1 }],
    ['cursor of another kind', { subreddit: 'golang', after: 't1_comment' }], ['cursor with uppercase', { subreddit: 'golang', after: 't3_ABC' }],
  ] as const)('rejects %s before the provider call', async (_name, input) => {
    const { client, requests } = newClient(() => json(200, listing(null)));
    expectBranch(await listPosts(client, input), 'defect', 'VALIDATION');
    expect(requests).toHaveLength(0);
  });
});

describe('searchPosts', () => {
  it.each([
    ['restricted to one subreddit', { query: 'generics "type sets"', subreddit: 'golang', sort: 'top', timeWindow: 'month', limit: 10, after: 't3_prev01' }, '/r/golang/search',
      { after: 't3_prev01', limit: '10', q: 'generics "type sets"', raw_json: '1', restrict_sr: 'true', sort: 'top', t: 'month', type: 'link' }],
    ['site-wide defaults', { query: 'generics' }, '/search', { limit: '25', q: 'generics', raw_json: '1', sort: 'new', t: 'all', type: 'link' }],
    ['comments sort', { query: 'generics', sort: 'comments', timeWindow: 'hour' }, '/search', { limit: '25', q: 'generics', raw_json: '1', sort: 'comments', t: 'hour', type: 'link' }],
  ] as const)('sends a search %s', async (_name, input, path, query) => {
    const { client, requests } = newClient(() => json(200, listing('t3_match01', post({ name: 't3_match01', title: 'Generics in practice' }))));
    const page = expectBranch(await searchPosts(client, input), 'searched');
    const url = new URL(requests[0]?.url ?? '');
    expect(url.pathname).toBe(path);
    expect(Object.fromEntries(url.searchParams)).toEqual(query);
    expect(page.after).toBe('t3_match01');
    expect(page.posts?.[0]?.title).toBe('Generics in practice');
  });

  it.each([
    ['blank query', { query: '' }], ['whitespace query', { query: ' \t' }], ['Go-space-only query', { query: '\u0085\u3000' }],
    ['513 characters', { query: '\u00e9'.repeat(MaxSearchQueryCharacters + 1) }],
    ['multi subreddit', { query: 'generics', subreddit: 'golang+programming' }], ['invalid subreddit', { query: 'generics', subreddit: 'r/golang' }],
    ['unknown sort', { query: 'generics', sort: 'oldest' }], ['unknown time window', { query: 'generics', timeWindow: 'decade' }],
    ['limit above page size', { query: 'generics', limit: MaxPostPageLimit + 1 }], ['cursor of another kind', { query: 'generics', after: 't5_subreddit' }],
  ] as const)('rejects %s before the provider call', async (_name, input) => {
    const { client, requests } = newClient(() => json(200, listing(null)));
    expectBranch(await searchPosts(client, input), 'defect', 'VALIDATION');
    expect(requests).toHaveLength(0);
  });

  it('accepts a query of exactly 512 characters', async () => {
    const { client, requests } = newClient(() => json(200, ''));
    expectBranch(await searchPosts(client, { query: '\u00e9'.repeat(MaxSearchQueryCharacters) }), 'invalidResponse', 'PROTOCOL');
    expect(requests).toHaveLength(1);
  });
});

describe('post page classification', () => {
  it.each([
    ['unknown name redirects to subreddit search', 302, { location: 'https://oauth.reddit.com/subreddits/search.json?q=nosuchcommunity' }, '', 'subredditUnavailable', 'NOT_FOUND'],
    ['relative redirect to subreddit search', 301, { location: '/subreddits/search?q=nosuchcommunity' }, '', 'subredditUnavailable', 'NOT_FOUND'],
    ['banned', 404, {}, '{"reason":"banned","message":"Not Found","error":404}', 'subredditUnavailable', 'NOT_FOUND'],
    ['private', 403, {}, '{"reason":"private","message":"Forbidden","error":403}', 'subredditUnavailable', 'AUTHORIZATION'],
    ['quarantined', 403, {}, '{"reason":"quarantined","message":"Forbidden","error":403}', 'subredditUnavailable', 'AUTHORIZATION'],
    ['forbidden without a reason', 403, {}, '{"message":"Forbidden","error":403}', 'providerRejected', 'PROVIDER_REJECTION'],
    ['unauthorized', 401, {}, '{"message":"Unauthorized","error":401}', 'providerRejected', 'PROVIDER_REJECTION'],
    ['redirect elsewhere', 302, { location: 'https://www.reddit.com/login' }, '', 'providerRejected', 'PROVIDER_REJECTION'],
  ] as const)('%s', async (_name, status, headers, body, branch, kind) => {
    const { client } = newClient(() => json(status, body, headers));
    expectBranch(await listPosts(client, { subreddit: 'nosuchcommunity' }), branch, kind);
    expectBranch(await searchPosts(client, { query: 'generics', subreddit: 'nosuchcommunity' }), branch, kind);
    // A site-wide search names no subreddit, so the same answer is a provider rejection.
    expectBranch(await searchPosts(client, { query: 'generics' }), 'providerRejected', 'PROVIDER_REJECTION');
  });

  it.each([
    ['429 with Retry-After', 429, { 'retry-after': '3', 'x-ratelimit-reset': '120' }, 'RATE_LIMIT', 3_000],
    ['429 with only the reset header', 429, { 'x-ratelimit-remaining': '0.0', 'x-ratelimit-reset': '7' }, 'RATE_LIMIT', 7_000],
    ['429 with a fractional reset', 429, { 'x-ratelimit-reset': '7.2' }, 'RATE_LIMIT', 8_000],
    ['429 with a reset beyond the cap', 429, { 'x-ratelimit-reset': '999999' }, 'RATE_LIMIT', 3_600_000],
    ['429 with an infinite reset', 429, { 'x-ratelimit-reset': '+Inf' }, 'RATE_LIMIT', 3_600_000],
    ['429 with an out-of-range reset', 429, { 'x-ratelimit-reset': '1e400' }, 'RATE_LIMIT', 0],
    ['429 with a malformed reset', 429, { 'x-ratelimit-reset': 'soon' }, 'RATE_LIMIT', 0],
    ['429 with digit separators', 429, { 'x-ratelimit-reset': '1_0' }, 'RATE_LIMIT', 10_000],
    ['429 with misplaced digit separators', 429, { 'x-ratelimit-reset': '1__0' }, 'RATE_LIMIT', 0],
    ['429 with a hexadecimal reset', 429, { 'x-ratelimit-reset': '0x1.8p1' }, 'RATE_LIMIT', 3_000],
    ['429 with a hexadecimal reset lacking an exponent', 429, { 'x-ratelimit-reset': '0x1' }, 'RATE_LIMIT', 0],
    ['429 with a reset padded by Go spaces', 429, { 'x-ratelimit-reset': '\u3000 7' }, 'RATE_LIMIT', 7_000],
    ['429 with a lowercase infinity', 429, { 'x-ratelimit-reset': 'infinity' }, 'RATE_LIMIT', 3_600_000],
    ['429 with a negative infinity', 429, { 'x-ratelimit-reset': '-Inf' }, 'RATE_LIMIT', 0],
    ['429 without headers', 429, {}, 'RATE_LIMIT', 0],
    ['503 ignores the reset header', 503, { 'x-ratelimit-reset': '7' }, 'AVAILABILITY', 0],
    ['500', 500, {}, 'AVAILABILITY', 0],
    ['408', 408, {}, 'AVAILABILITY', 0],
  ] as const)('retries %s for every operation', async (_name, status, headers, kind, delayMs) => {
    const { client, requests } = newClient(() => json(status, '', headers));
    const attempts = [
      await listPosts(client, { subreddit: 'golang' }), await searchPosts(client, { query: 'generics' }),
      await client.getPost().invoke(call('getPost'), { postId: 't3_abc123' }),
    ];
    for (const attempt of attempts) {
      expect(attempt.kind).toBe('retry');
      if (attempt.kind === 'retry') {
        expect(attempt.failure.kind).toBe(kind);
        expect(attempt.retryAfterMs).toBe(delayMs);
      }
    }
    expect(requests).toHaveLength(3);
  });

  it('retries transport failures', async () => {
    const { client } = newClient(() => { throw new ProviderTransportError(true); });
    for (const attempt of [await listPosts(client, { subreddit: 'golang' }), await searchPosts(client, { query: 'generics' })]) {
      expect(attempt.kind).toBe('retry');
      if (attempt.kind === 'retry') expect(attempt.failure.kind).toBe('TRANSPORT');
    }
  });

  const validPost = post();
  it.each([
    ['not JSON', '<html>busy</html>'], ['not a listing', { kind: 't3', data: {} }],
    ['null children', { kind: 'Listing', data: { after: null, children: null } }],
    ['comment child', { kind: 'Listing', data: { children: [{ kind: 't1', data: { name: 't1_abc' } }] } }],
    ['invalid post ID', listing(null, post({ name: 't3_UPPER' }))], ['blank title', listing(null, post({ title: '' }))],
    ['absolute permalink', listing(null, post({ permalink: 'https://evil.example/r/x' }))],
    ['zero creation time', listing(null, post({ created_utc: 0 }))], ['fractional score', listing(null, post({ score: 1.5 }))],
    ['string flag', listing(null, post({ stickied: 'true' }))],
    ['invalid after cursor', listing('t5_community', validPost)],
    ['more than 100 posts', listing(null, ...Array(MaxPostPageLimit + 1).fill(validPost))],
  ] as const)('selects invalidResponse for %s', async (_name, body) => {
    const { client } = newClient(() => json(200, body));
    expectBranch(await listPosts(client, { subreddit: 'golang' }), 'invalidResponse', 'PROTOCOL');
    expectBranch(await searchPosts(client, { query: 'generics' }), 'invalidResponse', 'PROTOCOL');
  });

  it('decodes missing and null fields as Go zero values', async () => {
    const { client } = newClient(() => json(200, listing(null, post({ author: null, url: undefined, selftext: null, score: null, over_18: null }))));
    const [item] = expectBranch(await listPosts(client, { subreddit: 'golang' }), 'listed').posts ?? [];
    expect(item).toMatchObject({ author: '', score: 0, isOver18: false });
    expect(item?.linkUrl).toBeUndefined();
  });

  it.each([
    ['uppercase scheme', 'HTTPS://example.com/a', 'HTTPS://example.com/a'], ['port', 'https://example.com:8443/a', 'https://example.com:8443/a'],
    ['invalid port', 'https://example.com:abc/a', undefined], ['empty host', 'https:///a', undefined],
    ['bad escape in path', 'https://example.com/%zz', undefined], ['bad escape in query only', 'https://example.com/a?b=%zz', 'https://example.com/a?b=%zz'],
    ['control character', 'https://example.com/\u0001', undefined], ['unbracketed host character', 'https://exa|mple.com/', undefined],
    ['host and port only', 'https://:80/a', 'https://:80/a'], ['IPv6 host', 'https://[::1]:80/a', 'https://[::1]:80/a'], ['unclosed IPv6 host', 'https://[::1/a', undefined],
    ['user information', 'https://user@example.com/a', 'https://user@example.com/a'], ['invalid user information', 'https://us|er@example.com/', undefined],
    ['bad escape in fragment', 'https://example.com/a#%zz', undefined], ['escaped ASCII in host', 'https://ex%41mple.com/', undefined],
    ['escaped multibyte host', 'https://ex%C3%A4mple.com/', 'https://ex%C3%A4mple.com/'], ['escaped percent in host', 'https://ex%25mple.com/', 'https://ex%25mple.com/'],
    ['IPv6 zone', 'https://[fe80::1%25en0]/', 'https://[fe80::1%25en0]/'], ['two colons', 'https://a:1:2/', 'https://a:1:2/'],
    ['unsupported scheme', 'ftp://example.com/', undefined], ['network-path reference', '//example.com/a', undefined],
  ] as const)('keeps a link URL with %s as Go url.Parse does', async (_name, value, expected) => {
    const { client } = newClient(() => json(200, listing(null, post({ url: value }))));
    expect(expectBranch(await listPosts(client, { subreddit: 'golang' }), 'listed').posts?.[0]?.linkUrl).toBe(expected);
  });

  it('bounds responses and keeps credentials out of results', async () => {
    const large = newClient(() => json(200, listing(null, post({ selftext: 'x'.repeat(4096) }))), 1024);
    expectBranch(await listPosts(large.client, { subreddit: 'golang' }), 'invalidResponse', 'RESPONSE_TOO_LARGE');
    const reflecting = newClient(() => json(200, listing(null, post({ selftext: `echo ${accessToken}` }))));
    const page = expectBranch(await searchPosts(reflecting.client, { query: 'generics' }), 'defect', 'AUTHENTICATION');
    expect(JSON.stringify(page)).not.toContain(accessToken);
  });
});
