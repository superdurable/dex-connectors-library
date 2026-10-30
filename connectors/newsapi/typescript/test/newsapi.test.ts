// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { afterEach, describe, expect, it } from 'vitest';
import { createServer, type RequestListener, type Server } from 'node:http';
import { once } from 'node:events';
import { SecretString, StaticCredentialProvider, runQuery, type Call, type QueryAttempt } from '@superdurable/connectors-sdk';
import { ArticlePageSchema, Client, newConnection, SearchArticlesBranchDefect, SearchArticlesBranchInvalidResponse, SearchArticlesBranchProviderRejected, SearchArticlesBranchSearched, SearchArticlesOperation, type ArticlePage } from '../src/index.js';
import { newProjectConnection } from '../src/index.js';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { Mock, searchArticlesInvalidKey, searchArticlesRetry, searchArticlesSearchedDefaultAnswer } from '../src/newsapimock.js';

const apiKey = '0123456789abcdef0123456789abcdef';
const connection = { provider: 'newsapi', name: 'newsapi' };
const call: Call = {
  context: { flowId: 'newsapi-test', cancellationSignal: new AbortController().signal } as Call['context'],
  id: '00000000-0000-5000-8000-000000000001', connection,
  operation: { connectorId: 'newsapi', operationId: 'searchArticles' },
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
};
const servers: Server[] = [];

afterEach(async () => { await Promise.all(servers.splice(0).map(server => new Promise<void>(resolve => server.close(() => resolve())))); });

async function provider(handler: RequestListener): Promise<string> {
  const server = createServer(handler);
  servers.push(server);
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (typeof address === 'string' || address === null) throw new Error('missing test address');
  return `http://127.0.0.1:${address.port}/v2`;
}

function operation(baseUrl: string, key = apiKey, maxResponseBytes?: number): SearchArticlesOperation {
  return new SearchArticlesOperation({ baseUrl, maxResponseBytes }, new StaticCredentialProvider([[connection, { apiKey: new SecretString(key) }]]));
}

function branch(attempt: QueryAttempt<ArticlePage>, expected: string, kind?: string): void {
  expect(attempt.kind).toBe('branch');
  if (attempt.kind === 'branch') { expect(attempt.branch).toBe(expected); expect(attempt.failure?.kind).toBe(kind); }
}

describe('NewsAPI searchArticles', () => {
  it('sends the key only in a header and preserves article nulls and UTC time', async () => {
    let requests = 0;
    const baseUrl = await provider((request, response) => {
      requests++;
      expect(request.url).toContain('/v2/everything?');
      expect(request.url).not.toContain(apiKey);
      expect(request.headers['x-api-key']).toBe(apiKey);
      const query = new URL(`http://example.test${request.url}`).searchParams;
      expect(Object.fromEntries(query)).toEqual({ q: '"AT&T" #1 + co', searchIn: 'title,description', sources: 'bbc-news,the-verge', domains: 'techcrunch.com', excludeDomains: 'example.com', from: '2026-09-25', to: '2026-10-05T07:00:00Z', language: 'en', sortBy: 'publishedAt', pageSize: '2', page: '2' });
      response.end(JSON.stringify({ status: 'ok', totalResults: 5, articles: [
        { source: { id: null, name: 'Example Wire' }, author: null, title: 'Example & partners', description: null, url: 'https://news.example/a', urlToImage: null, publishedAt: '2026-10-04T12:30:00Z', content: null },
        { source: { id: 'bbc-news', name: 'BBC News' }, author: 'Reporter', title: 'Second', description: '', url: 'https://news.example/b', urlToImage: 'https://news.example/b.jpg', publishedAt: '2026-10-04T05:00:00-07:00', content: 'Body' },
      ] }));
    });
    const attempt = await operation(baseUrl).invoke(call, { query: '"AT&T" #1 + co', searchIn: ['title', 'description'], sources: ['bbc-news', 'the-verge'], domains: ['techcrunch.com'], excludeDomains: ['example.com'], from: '2026-09-25', to: '2026-10-05T07:00:00Z', language: 'en', sortBy: 'publishedAt', pageSize: 2, page: 2 });
    branch(attempt, SearchArticlesBranchSearched);
    if (attempt.kind === 'branch') {
      expect(attempt.value.hasMore).toBe(true);
      expect(attempt.value.articles[0]?.description).toBeNull();
      expect(attempt.value.articles[1]?.description).toBe('');
      expect(attempt.value.articles[1]?.publishedAt).toBe('2026-10-04T12:00:00Z');
    }
    expect(requests).toBe(1);
  });

  it('omits empty search fields and returns removed articles unchanged', async () => {
    const baseUrl = await provider((request, response) => {
      expect(new URL(`http://example.test${request.url}`).searchParams.toString()).toBe('q=acme');
      response.end(JSON.stringify({ status: 'ok', totalResults: 101, articles: [{
        source: { id: null, name: '[Removed]' }, author: null, title: '[Removed]', description: '[Removed]',
        url: 'https://removed.example', urlToImage: null, publishedAt: '1970-01-01T00:00:00Z', content: '[Removed]',
      }] }));
    });
    const attempt = await operation(baseUrl).invoke(call, { query: 'acme' });
    branch(attempt, SearchArticlesBranchSearched);
    if (attempt.kind === 'branch') {
      expect(attempt.value.articles[0]?.title).toBe('[Removed]');
      expect(attempt.value.hasMore).toBe(true);
      expect(attempt.value.page).toBe(1);
    }
  });

  it('preserves Go zero values for omitted article fields', async () => {
    const baseUrl = await provider((_request, response) => response.end('{"status":"ok","totalResults":1,"articles":[{"title":"Sparse headline"}]}'));
    const attempt = await operation(baseUrl).invoke(call, { query: 'acme' });
    branch(attempt, SearchArticlesBranchSearched);
    if (attempt.kind === 'branch') {
      expect(attempt.value.articles[0]).toEqual({
        source: { id: null, name: '' }, author: null, title: 'Sparse headline', description: null,
        url: '', urlToImage: null, publishedAt: '0001-01-01T00:00:00Z', content: null,
      });
    }
  });

  it.each([
    [401, 'apiKeyInvalid', SearchArticlesBranchProviderRejected, 'AUTHENTICATION'],
    [400, 'apiKeyMissing', SearchArticlesBranchProviderRejected, 'AUTHENTICATION'],
    [429, 'apiKeyExhausted', SearchArticlesBranchProviderRejected, 'QUOTA_EXHAUSTED'],
    [400, 'sourceDoesNotExist', SearchArticlesBranchProviderRejected, 'VALIDATION'],
    [426, 'maximumResultsReached', SearchArticlesBranchProviderRejected, 'AUTHORIZATION'],
    [302, '', SearchArticlesBranchProviderRejected, 'PROVIDER_REJECTION'],
  ])('classifies status %i code %s', async (status, code, expectedBranch, kind) => {
    const baseUrl = await provider((_request, response) => {
      response.writeHead(status, { Location: '/elsewhere' });
      response.end(JSON.stringify({ status: 'error', code, message: 'SENTINEL' }));
    });
    const attempt = await operation(baseUrl).invoke(call, { query: 'acme' });
    branch(attempt, expectedBranch, kind);
    if (attempt.kind === 'branch') expect(attempt.failure?.message).not.toContain('SENTINEL');
  });

  it.each([[429, 'RATE_LIMIT'], [408, 'AVAILABILITY'], [503, 'AVAILABILITY']])('retries status %i with Retry-After', async (status, kind) => {
    const baseUrl = await provider((_request, response) => { response.writeHead(status, { 'Retry-After': '2' }); response.end('{"code":"rateLimited"}'); });
    const attempt = await operation(baseUrl).invoke(call, { query: 'acme' });
    expect(attempt.kind).toBe('retry');
    if (attempt.kind === 'retry') { expect(attempt.failure.kind).toBe(kind); expect(attempt.retryAfterMs).toBe(2000); }
  });

  it.each([
    [{}, 'VALIDATION'], [{ query: ' '.repeat(2) }, 'VALIDATION'], [{ query: 'x', searchIn: ['body'] }, 'VALIDATION'],
    [{ sources: ['BBC News'] }, 'VALIDATION'], [{ domains: ['x.com,y.com'] }, 'VALIDATION'],
    [{ query: 'x', from: '2026-10-02', to: '2026-10-01' }, 'VALIDATION'], [{ query: 'x', from: '2026-02-30' }, 'VALIDATION'], [{ query: 'x', pageSize: 101 }, 'VALIDATION'],
  ])('rejects invalid input before sending', async (input, kind) => {
    let requests = 0;
    const baseUrl = await provider((_request, response) => { requests++; response.end('unexpected'); });
    const attempt = await operation(baseUrl).invoke(call, input);
    branch(attempt, SearchArticlesBranchDefect, kind);
    expect(requests).toBe(0);
  });

  it('rejects missing and header-unsafe credentials before sending', async () => {
    let requests = 0;
    const baseUrl = await provider((_request, response) => { requests++; response.end('unexpected'); });
    branch(await operation(baseUrl, 'bad\nkey').invoke(call, { query: 'acme' }), SearchArticlesBranchDefect, 'AUTHENTICATION');
    const missing = new SearchArticlesOperation({ baseUrl }, new StaticCredentialProvider([]));
    branch(await missing.invoke(call, { query: 'acme' }), SearchArticlesBranchDefect, 'AUTHENTICATION');
    expect(requests).toBe(0);
  });

  it('retries a transport failure without a terminal branch', async () => {
    const attempt = await operation('http://127.0.0.1:1/v2').invoke(call, { query: 'acme' });
    expect(attempt.kind).toBe('retry');
    if (attempt.kind === 'retry') expect(attempt.failure.kind).toBe('AVAILABILITY');
  });

  it('validates local dependencies and configuration before network access', () => {
    expect(() => new SearchArticlesOperation({}, undefined as unknown as StaticCredentialProvider<{ apiKey: SecretString }>)).toThrow();
    expect(() => operation('http://example.com/v2')).toThrow();
    expect(() => new SearchArticlesOperation({ timeoutMs: -1 }, new StaticCredentialProvider([]))).toThrow();
    expect(() => new SearchArticlesOperation({ maxResponseBytes: -1 }, new StaticCredentialProvider([]))).toThrow();
  });

  it.each([
    ['not json', 'PROTOCOL'], ['{"status":"error","articles":[]}', 'PROTOCOL'],
    ['{"status":"ok","totalResults":1}', 'PROTOCOL'],
    ['{"status":"ok","totalResults":2,"articles":[]}', undefined],
  ])('classifies response %s', async (body, kind) => {
    const baseUrl = await provider((_request, response) => response.end(body));
    const attempt = await operation(baseUrl).invoke(call, { query: 'acme' });
    branch(attempt, kind ? SearchArticlesBranchInvalidResponse : SearchArticlesBranchSearched, kind);
  });

  it('rejects oversized responses', async () => {
    const baseUrl = await provider((_request, response) => response.end('x'.repeat(200)));
    branch(await operation(baseUrl, apiKey, 30).invoke(call, { query: 'acme' }), SearchArticlesBranchInvalidResponse, 'RESPONSE_TOO_LARGE');
  });

  it('classifies an oversized error body by its HTTP status', async () => {
    const baseUrl = await provider((_request, response) => { response.writeHead(503, { 'Retry-After': '3' }); response.end('x'.repeat(2_100_000)); });
    const attempt = await operation(baseUrl).invoke(call, { query: 'acme' });
    expect(attempt.kind).toBe('retry');
    if (attempt.kind === 'retry') { expect(attempt.failure.kind).toBe('AVAILABILITY'); expect(attempt.retryAfterMs).toBe(3000); }
  });

  it('returns a complete provider-safe receipt through runQuery', async () => {
    const baseUrl = await provider((_request, response) => response.end('{"status":"ok","totalResults":0,"articles":[]}'));
    const result = await runQuery(operation(baseUrl), call, { query: 'acme' });
    expect(result.receipt.callId).toBe(call.id);
    expect(result.receipt.provider).toBe('newsapi');
    expect(result.branch).toBe(SearchArticlesBranchSearched);
  });

  it('opens Dex Web project configuration and resolves the current key for every call', async () => {
    const keys = [apiKey, 'abcdef0123456789abcdef0123456789'];
    const baseUrl = await provider((request, response) => {
      expect(request.headers['x-api-key']).toBe(keys.shift());
      response.end('{"status":"ok","totalResults":0,"articles":[]}');
    });
    const project = {
      configuration: { connections: [{ connectorId: 'newsapi', connectionName: 'newsapi', configuration: { baseUrl, timeout: 90_000_000_000, maxResponseBytes: 1024 } }] },
      async resolveCredential() { return { api_key: keys[0] }; },
    } as unknown as LoadedProject;
    const configured = newProjectConnection(project, 'newsapi');
    expect(configured.client).toBeInstanceOf(Client);
    expect(configured.reference).toEqual(connection);
    branch(await configured.searchArticles.invoke(call, { query: 'acme' }), SearchArticlesBranchSearched);
    branch(await configured.searchArticles.invoke(call, { query: 'acme' }), SearchArticlesBranchSearched);
    expect(keys).toHaveLength(0);
  });

  it('uses manifest mock defaults and scripts retries and rejection', () => {
    const mock = new Mock('newsapi');
    expect(mock.connection.client).toBeUndefined();
    const defaultResult = mock.searchArticles.invoke(call, { query: 'Meridian' });
    branch(defaultResult, SearchArticlesBranchSearched);
    expect(searchArticlesSearchedDefaultAnswer().kind).toBe('branch');
    expect(ArticlePageSchema.parse(defaultResult.kind === 'branch' ? defaultResult.value : null).articles).toHaveLength(2);
    mock.searchArticles.respond(searchArticlesRetry({ kind: 'AVAILABILITY', provider: 'newsapi', operation: 'searchArticles', message: 'outage' }, 1000));
    mock.searchArticles.respond(searchArticlesInvalidKey());
    expect(mock.searchArticles.invoke(call, { query: 'Meridian' }).kind).toBe('retry');
    branch(mock.searchArticles.invoke(call, { query: 'Meridian' }), SearchArticlesBranchProviderRejected, 'AUTHENTICATION');
    expect(mock.searchArticles.calls()).toHaveLength(3);
  });

  it('binds a validated client and reference before a Step is constructed', () => {
    const client = new Client({}, new StaticCredentialProvider([]));
    const bound = newConnection(client, connection);
    expect(bound.client).toBe(client);
    expect(bound.searchArticles).toBe(client.searchArticles);
    expect(() => newConnection(client, { provider: 'other', name: 'newsapi' })).toThrow();
  });
});
