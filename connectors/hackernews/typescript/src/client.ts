// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { parseProviderJSON, strictInteger, fieldError, goTimestamp, failure, failed } from './provider-response.js';
import { SearchItemsOperation, SearchStoriesOperation } from './search.js';
export { MaxStoryTextCharacters, SearchItemsOperation, SearchStoriesOperation } from './search.js';
import { GetArticleThreadDefinition, GetUserDefinition, GetItemDefinition, ListStoryIDsDefinition } from './generated/definitions.js';
import type { ArticleComment, ArticleThread, GetArticleThreadInput, Config, GetUserInput, User, GetItemInput, Item, ListStoryIDsInput, StoryIDs } from './generated/models.js';

const defaultBaseUrl = 'https://hacker-news.firebaseio.com/v0';
const defaultSearchBaseUrl = 'https://hn.algolia.com/api/v1';
const zeroTime = '0001-01-01T00:00:00Z';
const zeroIds: StoryIDs = { ids: [], hasMore: false, observedAt: zeroTime };
const zeroUser: User = { id: '', created: 0, karma: 0, profileUrl: '' };
const zeroThread: ArticleThread = { id: 0, title: '', discussionUrl: '', comments: [] };
const zeroItem: Item = { id: 0, type: '', discussionUrl: '' };
const validFeeds = new Set(['top', 'best', 'new', 'ask', 'show']);
const validItemTypes = new Set(['story', 'comment', 'job', 'poll', 'pollopt']);
/** Injectable HTTP sender and clock for deterministic provider replay. */
export interface HackerNewsClientOptions {
  /** Send one bounded request without redirect following. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock for observed feed time and Retry-After. */
  now?: () => Date;
}

/** Public Hacker News API client bound to immutable configuration. */
export class HackerNewsClient {
  private readonly baseUrl: string;
  private readonly searchBaseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: NonNullable<HackerNewsClientOptions['send']>;
  readonly now: () => Date;

  /** Validate configuration before making requests. */
  constructor(config: Config, options: HackerNewsClientOptions = {}) {
    if (!config) throw new TypeError('Hacker News configuration is required');
    this.baseUrl = validateBaseURL(config.baseUrl || defaultBaseUrl);
    this.searchBaseUrl = validateBaseURL(config.searchBaseUrl || defaultSearchBaseUrl);
    this.timeoutMs = config.timeoutMs === undefined || config.timeoutMs === 0 ? 15_000 : config.timeoutMs;
    this.maxResponseBytes = config.maxResponseBytes === undefined || config.maxResponseBytes === 0 ? 1_048_576 : config.maxResponseBytes;
    if (!Number.isFinite(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) {
      throw new TypeError('Hacker News timeout and maxResponseBytes must be positive');
    }
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the feed Query. */
  listStoryIDs(): ListStoryIDsOperation { return new ListStoryIDsOperation(this); }
  /** Bind the item Query. */
  getItem(): GetItemOperation { return new GetItemOperation(this); }
  /** Bind the indexed article and public comment tree Query. */
  getArticleThread(): GetArticleThreadOperation { return new GetArticleThreadOperation(this); }
  /** Bind the public user profile Query. */
  getUser(): GetUserOperation { return new GetUserOperation(this); }
  /** Bind the mixed public item search Query. */
  searchItems(): SearchItemsOperation { return new SearchItemsOperation(this); }
  /** Bind the story search Query. */
  searchStories(): SearchStoriesOperation { return new SearchStoriesOperation(this); }

  /** Send one Hacker News API read and classify its HTTP or transport outcome. */
  getJSON<Output>(call: Call, operation: string, path: string, zero: Output, decode: (body: Uint8Array) => QueryAttempt<Output>): Promise<QueryAttempt<Output>> {
    return this.sendJSON(call, operation, this.baseUrl + path, this.baseUrl === defaultBaseUrl, zero, decode);
  }

  /** Send one Hacker News Search API read and classify its HTTP or transport outcome. */
  getSearchJSON<Output>(call: Call, operation: string, pathAndQuery: string, zero: Output, decode: (body: Uint8Array) => QueryAttempt<Output>): Promise<QueryAttempt<Output>> {
    return this.sendJSON(call, operation, this.searchBaseUrl + pathAndQuery, this.searchBaseUrl === defaultSearchBaseUrl, zero, decode);
  }

  private async sendJSON<Output>(call: Call, operation: string, url: string, isPublicEndpoint: boolean, zero: Output, decode: (body: Uint8Array) => QueryAttempt<Output>): Promise<QueryAttempt<Output>> {
    let response: ProviderResponse;
    try {
      response = await this.send({
        url, method: 'GET', headers: { Accept: 'application/json', 'User-Agent': 'superdurable-hacker-news-daily/0.1' },
        maxResponseBytes: this.maxResponseBytes,
        addressPolicy: isPublicEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP',
        timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        const status = error.status ?? 0;
        if (status === 408 || status === 429 || status >= 500) {
          const retryAfter = error.headers?.['retry-after'];
          return newQueryRetry(failure(operation, status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', 'Hacker News is temporarily unavailable'), parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '', this.now()));
        }
        if (status === 404 && (operation === 'getItem' || operation === 'getUser' || operation === 'getArticleThread')) return failed(operation, 'notFound', zero, 'NOT_FOUND', 'Hacker News resource was not found');
        if (status !== 200) return failed(operation, 'providerRejected', zero, 'PROVIDER_REJECTION', 'Hacker News rejected the request');
        return failed(operation, 'invalidResponse', zero, 'RESPONSE_TOO_LARGE', 'Hacker News response exceeded the configured limit');
      }
      const isInterruptedResponse = (error instanceof ProviderTransportError || error instanceof ProviderStalledError) && error.status === 200;
      return newQueryRetry(failure(operation, 'AVAILABILITY', isInterruptedResponse ? 'Hacker News response was interrupted' : 'Hacker News request failed'), 0);
    }
    const status = response.status;
    if (status === 408 || status === 429 || status >= 500) {
      const retryAfter = response.headers['retry-after'];
      return newQueryRetry(failure(operation, status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', 'Hacker News is temporarily unavailable'), parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '', this.now()));
    }
    if (status === 404 && (operation === 'getItem' || operation === 'getUser' || operation === 'getArticleThread')) return failed(operation, 'notFound', zero, 'NOT_FOUND', 'Hacker News resource was not found');
    if (status !== 200) return failed(operation, 'providerRejected', zero, 'PROVIDER_REJECTION', 'Hacker News rejected the request');
    return decode(response.body);
  }
}

/** Read a bounded feed snapshot. */
export class ListStoryIDsOperation implements Query<ListStoryIDsInput, StoryIDs> {
  /** Bind the owning client. */ constructor(private readonly client: HackerNewsClient) {}
  /** Return manifest operation metadata. */ definition() { return ListStoryIDsDefinition; }
  /** Validate the feed and decode one provider response. */
  invoke(call: Call, input: ListStoryIDsInput): Promise<QueryAttempt<StoryIDs>> {
    const feed = input.feed || 'top';
    const limit = input.limit || 50;
    if (!validFeeds.has(feed)) return Promise.resolve(failed('listStoryIDs', 'defect', zeroIds, 'VALIDATION', 'feed must be top, best, new, ask, or show'));
    if (!Number.isInteger(limit) || limit < 1 || limit > 500) return Promise.resolve(failed('listStoryIDs', 'defect', zeroIds, 'VALIDATION', 'limit must be from 1 through 500'));
    return this.client.getJSON(call, 'listStoryIDs', `/${feed}stories.json`, zeroIds, body => {
      let ids: unknown;
      try { ids = parseProviderJSON(body); } catch { ids = undefined; }
      if (!Array.isArray(ids)) return failed('listStoryIDs', 'invalidResponse', zeroIds, 'PROTOCOL', 'Hacker News returned an invalid feed');
      const feedIDs: number[] = [];
      for (const id of ids) {
        if (id === null) return failed('listStoryIDs', 'invalidResponse', zeroIds, 'PROTOCOL', 'Hacker News returned an invalid item ID');
        try { feedIDs.push(strictInteger(id, 'item ID')); }
        catch (error) { return failed('listStoryIDs', 'invalidResponse', zeroIds, 'PROTOCOL', `Hacker News returned an invalid feed: ${fieldError(error)}`); }
      }
      if (feedIDs.some(id => id <= 0)) return failed('listStoryIDs', 'invalidResponse', zeroIds, 'PROTOCOL', 'Hacker News returned an invalid item ID');
      return newQueryBranch('listed', { ids: feedIDs.slice(0, limit), hasMore: feedIDs.length > limit, observedAt: goTimestamp(this.client.now()) }, {});
    });
  }
}

/** Read one indexed article with its public comments. */
export class GetArticleThreadOperation implements Query<GetArticleThreadInput, ArticleThread> {
  /** Bind the owning client. */
  constructor(private readonly client: HackerNewsClient) {}
  /** Return the manifest Query contract. */
  definition() { return GetArticleThreadDefinition; }
  /** Validate the article ID and read its tree in one bounded request. */
  invoke(call: Call, input: GetArticleThreadInput): Promise<QueryAttempt<ArticleThread>> {
    if (!Number.isSafeInteger(input.id) || input.id <= 0) {
      return Promise.resolve(failed('getArticleThread', 'defect', zeroThread, 'VALIDATION', 'article ID must be positive'));
    }
    return this.client.getSearchJSON(call, 'getArticleThread', `/items/${input.id}`, zeroThread, body => decodeArticleThread(body, input.id));
  }
}

function decodeArticleThread(body: Uint8Array, requestedId: number): QueryAttempt<ArticleThread> {
  const operation = 'getArticleThread';
  try {
    const document = parseProviderJSON(body);
    if (document === null) return failed(operation, 'notFound', zeroThread, 'NOT_FOUND', 'Hacker News article was not found');
    const article = readArticleNode(document);
    if (article.id !== requestedId || !article.title) return failed(operation, 'invalidResponse', zeroThread, 'PROTOCOL', 'Hacker News Search returned an invalid article');
    const result: ArticleThread = {
      id: article.id, title: article.title, discussionUrl: discussionURL(article.id),
      comments: new CommentTreeReader().readChildren(article, 0),
    };
    if (article.author) result.author = article.author;
    if (article.url) result.url = article.url;
    if (article.text) result.textHtml = article.text;
    return newQueryBranch('found', result, {});
  } catch (error) {
    return failed(operation, 'invalidResponse', zeroThread, 'PROTOCOL', error instanceof InvalidCommentTree ?
      'Hacker News Search returned an invalid comment tree' : 'Hacker News Search returned malformed article JSON');
  }
}

class InvalidCommentTree extends Error {}

interface ArticleNode {
  id: number;
  parentId: number;
  title: string;
  url: string;
  author: string;
  text: string;
  children: unknown[];
}

function readArticleNode(value: unknown): ArticleNode {
  const node: ArticleNode = { id: 0, parentId: 0, title: '', url: '', author: '', text: '', children: [] };
  if (value === null) return node;
  if (typeof value !== 'object' || Array.isArray(value)) throw new TypeError('article node');
  for (const [key, field] of Object.entries(value)) {
    if (field === null) continue;
    const name = key.toLowerCase();
    switch (name) {
      case 'id': node.id = strictInteger(field, name); break;
      case 'parent_id': node.parentId = strictInteger(field, name); break;
      case 'title': case 'author': case 'url': case 'text':
        if (typeof field !== 'string') throw new TypeError(name);
        node[name] = field;
        break;
      case 'children':
        if (!Array.isArray(field)) throw new TypeError(name);
        node.children = field;
    }
  }
  return node;
}

class CommentTreeReader {
  private remaining = 5000;

  readChildren(parent: ArticleNode, depth: number): ArticleComment[] {
    if (depth >= 64 || parent.children.length > this.remaining) throw new InvalidCommentTree();
    this.remaining -= parent.children.length;
    return parent.children.map(child => {
      const node = readArticleNode(child);
      if (node.id <= 0 || (node.parentId !== 0 && node.parentId !== parent.id)) throw new InvalidCommentTree();
      const comment: ArticleComment = { id: node.id, discussionUrl: discussionURL(node.id) };
      if (node.author) comment.author = node.author;
      if (node.text) comment.textHtml = node.text;
      const replies = this.readChildren(node, depth + 1);
      if (replies.length) comment.replies = replies;
      return comment;
    });
  }
}

function discussionURL(id: number): string { return `https://news.ycombinator.com/item?id=${id}`; }

/** Read one Hacker News story, comment, job, poll, or poll option. */
export class GetItemOperation implements Query<GetItemInput, Item> {
  /** Bind the owning client. */ constructor(private readonly client: HackerNewsClient) {}
  /** Return manifest operation metadata. */ definition() { return GetItemDefinition; }
  /** Validate identity and decode one item response. */
  invoke(call: Call, input: GetItemInput): Promise<QueryAttempt<Item>> {
    if (!Number.isSafeInteger(input.id) || input.id <= 0) return Promise.resolve(failed('getItem', 'defect', zeroItem, 'VALIDATION', 'item ID must be positive'));
    return this.client.getJSON(call, 'getItem', `/item/${input.id}.json`, zeroItem, body => {
      let value: unknown;
      try { value = parseProviderJSON(body); } catch { return failed('getItem', 'invalidResponse', zeroItem, 'PROTOCOL', 'Hacker News returned malformed item JSON'); }
      if (value === null) return failed('getItem', 'notFound', zeroItem, 'NOT_FOUND', 'Hacker News item is deleted or dead');
      let item: Item;
      try { item = decodeItem(value); } catch (error) { return failed('getItem', 'invalidResponse', zeroItem, 'PROTOCOL', `Hacker News returned malformed item JSON: ${fieldError(error)}`); }
      if (item.deleted || item.dead) return failed('getItem', 'notFound', zeroItem, 'NOT_FOUND', 'Hacker News item is deleted or dead');
      if (item.id !== input.id) return failed('getItem', 'invalidResponse', zeroItem, 'PROTOCOL', 'Hacker News returned a different item ID');
      if (!validItemTypes.has(item.type)) return failed('getItem', 'invalidResponse', zeroItem, 'PROTOCOL', 'Hacker News returned an unknown item type');
      if (item.kids?.some(id => id <= 0)) return failed('getItem', 'invalidResponse', zeroItem, 'PROTOCOL', 'Hacker News returned an invalid child ID');
      return newQueryBranch('found', item, {});
    });
  }
}

/** Read one public profile by case-sensitive Hacker News username. */
export class GetUserOperation implements Query<GetUserInput, User> {
  /** Bind the owning client. */ constructor(private readonly client: HackerNewsClient) {}
  /** Return the manifest's profile Query contract. */ definition() { return GetUserDefinition; }

  /** Read one public profile and build its canonical Hacker News permalink. */
  invoke(call: Call, input: GetUserInput): Promise<QueryAttempt<User>> {
    const id = input?.id;
    if (typeof id !== 'string' || !id || Buffer.byteLength(id) > 100 || /[/?#]/.test(id) || /[\uD800-\uDFFF]/u.test(id)) {
      return Promise.resolve(failed('getUser', 'defect', zeroUser, 'VALIDATION', 'username is invalid'));
    }
    const escaped = encodeURIComponent(id).replace(/[!'()*]/g, char => '%' + char.charCodeAt(0).toString(16).toUpperCase());
    const segment = escaped.replace(/%(24|26|2B|3A|3D|40)/g, match => decodeURIComponent(match));
    return this.client.getJSON(call, 'getUser', `/user/${segment}.json`, zeroUser, body => {
      let user: User;
      try {
        const document = parseProviderJSON(body);
        if (document === null) return failed('getUser', 'notFound', zeroUser, 'NOT_FOUND', 'Hacker News user was not found');
        user = decodeUser(document);
      } catch {
        return failed('getUser', 'invalidResponse', zeroUser, 'PROTOCOL', 'Hacker News returned malformed user JSON');
      }
      if (user.id !== id || user.created < 0 || user.karma < 0)
        return failed('getUser', 'invalidResponse', zeroUser, 'PROTOCOL', 'Hacker News returned invalid user data');
      if (user.submitted?.some(item => item <= 0))
        return failed('getUser', 'invalidResponse', zeroUser, 'PROTOCOL', 'Hacker News returned an invalid submitted item ID');
      user.profileUrl = 'https://news.ycombinator.com/user?id=' + escaped.replace(/%20/g, '+');
      return newQueryBranch('found', user, {});
    });
  }
}

function decodeUser(document: unknown): User {
  if (!document || typeof document !== 'object' || Array.isArray(document)) throw new TypeError('profile');
  const fields: Record<string, unknown> = Object.fromEntries(Object.entries(document).map(([name, value]) => [name.toLowerCase(), value]));
  const username = fields.id ?? '';
  const about = fields.about ?? '';
  if (typeof username !== 'string' || typeof about !== 'string' || typeof (fields.profileurl ?? '') !== 'string') throw new TypeError('profile fields');
  const user: User = { id: username, created: fields.created == null ? 0 : strictInteger(fields.created, 'created'),
    karma: fields.karma == null ? 0 : strictInteger(fields.karma, 'karma'), profileUrl: '' };
  if (about) user.about = about;
  const submitted = fields.submitted ?? [];
  if (!Array.isArray(submitted)) throw new TypeError('submitted');
  if (submitted.length) user.submitted = submitted.map(value => value === null ? 0 : strictInteger(value, 'submitted'));
  return user;
}

function decodeItem(value: unknown): Item {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('item must be an object');
  const document = value as Record<string, unknown>;
  const string = (name: string) => { const field = document[name]; if (field === undefined || field === null) return ''; if (typeof field !== 'string') throw new TypeError(name); return field; };
  const integer = (name: string) => { const field = document[name]; if (field === undefined || field === null) return 0; return strictInteger(field, name); };
  const boolean = (name: string) => { const field = document[name]; if (field === undefined || field === null) return false; if (typeof field !== 'boolean') throw new TypeError(name); return field; };
  const kids = document.kids;
  if (kids !== undefined && kids !== null && !Array.isArray(kids)) throw new TypeError('kids');
  const childIDs = Array.isArray(kids) ? kids.map(id => id === null ? 0 : strictInteger(id, 'kids')) : [];
  const id = integer('id');
  const item: Item = { id, type: string('type'), discussionUrl: `https://news.ycombinator.com/item?id=${id}` };
  for (const name of ['by', 'title', 'text', 'url'] as const) { const field = string(name); if (field) item[name] = field; }
  for (const name of ['time', 'score', 'descendants', 'parent'] as const) { const field = integer(name); if (field) item[name] = field; }
  if (childIDs.length) item.kids = childIDs;
  if (boolean('deleted')) item.deleted = true;
  if (boolean('dead')) item.dead = true;
  return item;
}
