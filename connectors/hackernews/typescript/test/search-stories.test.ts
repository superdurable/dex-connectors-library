// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { HackerNewsClient, MaxStoryTextCharacters, SearchStoriesInputSchema, type SearchStoriesInput } from '../src/index.js';

const call = { context: { cancellationSignal: new AbortController().signal } } as Call;
const emptyPage = (page = 0) => `{"hits":[],"page":${page},"nbPages":0,"nbHits":0}`;

/** A client whose provider records each request and answers with one fixed response. */
function recordingClient(response: (request: ProviderRequest) => ProviderResponse, config: { searchBaseUrl?: string } = {}) {
  const requests: ProviderRequest[] = [];
  const client = new HackerNewsClient({ searchBaseUrl: 'http://127.0.0.1:8849', ...config }, {
    now: () => new Date('2026-10-26T12:00:00Z'),
    send: async request => { requests.push(request); return response(request); },
  });
  return { client, requests };
}

function jsonResponse(body: string, status = 200, headers: Record<string, string> = {}): ProviderResponse {
  return { status, headers, body: Buffer.from(body) };
}

describe('searchStories', () => {
  for (const [name, input, path, query] of [
    ['defaults', {}, '/search', { tags: 'story', page: '0', hitsPerPage: '20' }],
    ['relevance', { query: 'rust compiler', sortBy: 'relevance' }, '/search', { query: 'rust compiler', tags: 'story', page: '0', hitsPerPage: '20' }],
    ['newest show and front page', {
      query: 'postgres', sortBy: 'date', tags: ['show_hn', 'story', 'show_hn', 'front_page'], minPoints: 10, page: 2, hitsPerPage: 100,
      createdAfter: '2026-10-25T12:00:00.5Z', createdBefore: '2026-10-26T12:00:00.5+01:00',
    }, '/search_by_date', {
      query: 'postgres', tags: 'story,show_hn,front_page', page: '2', hitsPerPage: '100',
      // Fractional bounds round outward to whole seconds, which keeps both comparisons strict.
      numericFilters: 'created_at_i>1792929600,created_at_i<1793012401,points>=10',
    }],
    ['whole-second bounds', { createdAfter: '2026-10-26T12:00:00Z', createdBefore: '2026-10-27T12:00:00Z', tags: ['ask_hn'] }, '/search', {
      tags: 'story,ask_hn', page: '0', hitsPerPage: '20', numericFilters: 'created_at_i>1793016000,created_at_i<1793102400',
    }],
    ['zero time is unset', { createdAfter: '0001-01-01T00:00:00Z' }, '/search', { tags: 'story', page: '0', hitsPerPage: '20' }],
  ] as const) {
    it(`sends the ${name} request`, async () => {
      const parsed = SearchStoriesInputSchema.parse(input);
      const { client, requests } = recordingClient(() => jsonResponse(emptyPage(parsed.page ?? 0)));
      const result = await client.searchStories().invoke(call, parsed);
      expect(result).toMatchObject({ branch: 'searched', value: { stories: [] } });
      expect(requests).toHaveLength(1);
      const url = new URL(requests[0]!.url);
      expect(url.pathname).toBe(path);
      expect(Object.fromEntries(url.searchParams)).toEqual(query);
      expect(requests[0]!.headers).toMatchObject({ Accept: 'application/json' });
      expect(requests[0]!.headers).not.toHaveProperty('Authorization');
      expect(requests[0]!.addressPolicy).toBe('privateNetworkHTTP');
    });
  }

  it('keeps the configured path prefix and uses the public policy for the default endpoint', async () => {
    const prefixed = recordingClient(() => jsonResponse(emptyPage()), { searchBaseUrl: 'http://127.0.0.1:8849/api/v1/' });
    await prefixed.client.searchStories().invoke(call, { sortBy: 'date' });
    expect(new URL(prefixed.requests[0]!.url).pathname).toBe('/api/v1/search_by_date');
    const requests: ProviderRequest[] = [];
    const publicClient = new HackerNewsClient({}, { send: async request => { requests.push(request); return jsonResponse(emptyPage()); } });
    await publicClient.searchStories().invoke(call, { query: 'postgres' });
    expect(requests[0]!.url.startsWith('https://hn.algolia.com/api/v1/search?')).toBe(true);
    expect(requests[0]!.addressPolicy).toBe('publicHTTPS');
  });

  it('decodes hits, plain story text, its bound, and paging', async () => {
    const longText = 'é'.repeat(MaxStoryTextCharacters + 5);
    const { client } = recordingClient(request => jsonResponse(`{"hits":[
      {"objectID":"42003","title":"Show HN: A compiler explorer","url":"https://example.com/explorer","author":"langtools","points":12,"num_comments":3,
       "created_at_i":1793015400,"story_text":null,"_tags":["show_hn","author_langtools","story","story_42003"]},
      {"objectID":"41990","title":"Ask HN: How do you test a compiler?","url":null,"author":"testwriter","points":null,"num_comments":null,
       "created_at_i":1793007000,"story_text":"We fuzz the parser &amp; the <i>optimizer</i>.<p>See <a href=\\"https:&#x2F;&#x2F;example.com&#x2F;fuzz\\">https:&#x2F;&#x2F;example.com&#x2F;fuzz</a>\\n  for details.<p><pre><code>  run   --all\\n</code></pre>","_tags":["story","ask_hn","front_page"]},
      {"objectID":"41980","title":"Long notes","created_at_i":1793000100,"story_text":"${longText}","_tags":null}
    ],"page":${new URL(request.url).searchParams.get('page')},"nbPages":3,"nbHits":5,"hitsPerPage":3}`));
    const first = await client.searchStories().invoke(call, { query: 'compiler', hitsPerPage: 3 });
    expect(first).toMatchObject({ branch: 'searched' });
    expect(first.kind === 'branch' && first.value).toEqual({
      page: 0, pages: 3, totalHits: 5, hitsPerPage: 3, nextPage: 1, stories: [
        {
          id: 42003, title: 'Show HN: A compiler explorer', url: 'https://example.com/explorer', discussionUrl: 'https://news.ycombinator.com/item?id=42003',
          author: 'langtools', points: 12, commentCount: 3, createdAt: '2026-10-26T11:50:00Z', tags: ['story', 'show_hn'],
        },
        {
          id: 41990, title: 'Ask HN: How do you test a compiler?', discussionUrl: 'https://news.ycombinator.com/item?id=41990', author: 'testwriter',
          points: 0, commentCount: 0, createdAt: '2026-10-26T09:30:00Z', tags: ['story', 'ask_hn', 'front_page'],
          storyText: 'We fuzz the parser & the optimizer.\n\nSee https://example.com/fuzz for details.\n\nrun --all',
        },
        {
          id: 41980, title: 'Long notes', discussionUrl: 'https://news.ycombinator.com/item?id=41980', author: '', points: 0, commentCount: 0,
          createdAt: '2026-10-26T07:35:00Z', tags: [], storyText: 'é'.repeat(MaxStoryTextCharacters), isStoryTextTruncated: true,
        },
      ],
    });
    const last = await client.searchStories().invoke(call, { query: 'compiler', hitsPerPage: 3, page: 2 });
    expect(last).toMatchObject({ branch: 'searched', value: { page: 2, nextPage: 0 } });
  });

  it('keeps invalid input local and accepts the largest valid query and page', async () => {
    const { client, requests } = recordingClient(() => jsonResponse(emptyPage(49)));
    const createdAt = '2026-10-26T12:00:00Z';
    for (const input of [
      { sortBy: 'popularity' }, { tags: ['comment'] }, { tags: ['author_pg'] }, { query: 'a'.repeat(513) }, { query: 'é'.repeat(257) },
      { minPoints: -1 }, { createdAfter: createdAt, createdBefore: createdAt }, { createdAfter: createdAt, createdBefore: '2026-10-26T11:00:00Z' },
      { hitsPerPage: 101 }, { hitsPerPage: -1 }, { page: -1 }, { page: 50 }, { page: 2 ** 40, hitsPerPage: 100 },
    ] satisfies SearchStoriesInput[]) {
      expect(await client.searchStories().invoke(call, input)).toMatchObject({ branch: 'defect', failure: { kind: 'VALIDATION' } });
    }
    expect(requests).toHaveLength(0);
    expect(await client.searchStories().invoke(call, { query: 'a'.repeat(512), page: 49 })).toMatchObject({ branch: 'searched' });
    expect(requests).toHaveLength(1);
  });

  for (const [name, body] of [
    ['bad JSON', '{'], ['null', 'null'], ['array', '[]'], ['null hits', '{"hits":null,"page":0,"nbPages":0,"nbHits":0}'],
    ['missing page', '{"hits":[],"nbPages":0,"nbHits":0}'], ['missing page count', '{"hits":[],"page":0,"nbHits":0}'],
    ['missing hit count', '{"hits":[],"page":0,"nbPages":0}'], ['fractional page', '{"hits":[],"page":0.5,"nbPages":0,"nbHits":0}'],
    ['other page', '{"hits":[],"page":1,"nbPages":2,"nbHits":2}'], ['negative hit count', '{"hits":[],"page":0,"nbPages":0,"nbHits":-1}'],
    ['too many hits', '{"hits":[{"objectID":"1","created_at_i":1},{"objectID":"2","created_at_i":1}],"page":0,"nbPages":1,"nbHits":2}'],
    ['numeric story ID', '{"hits":[{"objectID":1,"created_at_i":1}],"page":0,"nbPages":1,"nbHits":1}'],
    ['text story ID', '{"hits":[{"objectID":"abc","created_at_i":1}],"page":0,"nbPages":1,"nbHits":1}'],
    ['zero story ID', '{"hits":[{"objectID":"0","created_at_i":1}],"page":0,"nbPages":1,"nbHits":1}'],
    ['null hit', '{"hits":[null],"page":0,"nbPages":1,"nbHits":1}'],
    ['missing creation time', '{"hits":[{"objectID":"1"}],"page":0,"nbPages":1,"nbHits":1}'],
    ['text points', '{"hits":[{"objectID":"1","created_at_i":1,"points":"5"}],"page":0,"nbPages":1,"nbHits":1}'],
    ['numeric tag', '{"hits":[{"objectID":"1","created_at_i":1,"_tags":[1]}],"page":0,"nbPages":1,"nbHits":1}'],
  ] as const) {
    it(`selects invalidResponse for ${name}`, async () => {
      const { client } = recordingClient(() => jsonResponse(body));
      expect(await client.searchStories().invoke(call, { query: 'compiler', hitsPerPage: 1 })).toMatchObject({ branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
    });
  }

  it('selects invalidResponse for an oversized page', async () => {
    const client = new HackerNewsClient({}, { send: async () => { throw new BodyTooLargeError(200, {}); } });
    expect(await client.searchStories().invoke(call, { query: 'compiler' })).toMatchObject({ branch: 'invalidResponse', failure: { kind: 'RESPONSE_TOO_LARGE' } });
  });

  it('accepts a signed story ID as Go strconv.ParseInt does', async () => {
    const { client } = recordingClient(() => jsonResponse('{"hits":[{"objectID":"+042","created_at_i":1}],"page":0,"nbPages":1,"nbHits":1}'));
    expect(await client.searchStories().invoke(call, { hitsPerPage: 1 })).toMatchObject({ branch: 'searched', value: { stories: [{ id: 42 }] } });
  });

  for (const [status, expected] of [
    [429, { kind: 'retry', failure: { kind: 'RATE_LIMIT' }, retryAfterMs: 2000 }], [500, { kind: 'retry', failure: { kind: 'AVAILABILITY' }, retryAfterMs: 2000 }],
    [503, { kind: 'retry', retryAfterMs: 2000 }], [408, { kind: 'retry', retryAfterMs: 2000 }],
    [400, { branch: 'providerRejected' }], [403, { branch: 'providerRejected' }], [404, { branch: 'providerRejected' }], [302, { branch: 'providerRejected' }],
  ] as const) {
    it(`classifies HTTP ${status}`, async () => {
      const { client } = recordingClient(() => jsonResponse('{}', status, { 'retry-after': '2', location: '/unexpected' }));
      expect(await client.searchStories().invoke(call, { query: 'compiler' })).toMatchObject(expected);
    });
  }

  it('validates the search base URL', () => {
    for (const searchBaseUrl of ['http://example.com/api/v1', 'https://example.com/api/v1?x=1', 'ftp://example.com']) {
      expect(() => new HackerNewsClient({ searchBaseUrl })).toThrow();
    }
  });
});
