// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetItemDefinition, ListStoryIDsDefinition } from './generated/definitions.js';
import type { Config, GetItemInput, Item, ListStoryIDsInput, StoryIDs } from './generated/models.js';

const defaultBaseUrl = 'https://hacker-news.firebaseio.com/v0';
const zeroTime = '0001-01-01T00:00:00Z';
const zeroIds: StoryIDs = { ids: [], hasMore: false, observedAt: zeroTime };
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
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: NonNullable<HackerNewsClientOptions['send']>;
  readonly now: () => Date;

  /** Validate configuration before making requests. */
  constructor(config: Config, options: HackerNewsClientOptions = {}) {
    if (!config) throw new TypeError('Hacker News configuration is required');
    this.baseUrl = validateBaseURL(config.baseUrl || defaultBaseUrl);
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

  /** Send one read and classify its HTTP or transport outcome. */
  async getJSON<Output>(call: Call, operation: string, path: string, zero: Output, decode: (body: Uint8Array) => QueryAttempt<Output>): Promise<QueryAttempt<Output>> {
    let response: ProviderResponse;
    try {
      response = await this.send({
        url: this.baseUrl + path, method: 'GET', headers: { Accept: 'application/json', 'User-Agent': 'superdurable-hacker-news-daily/0.1' },
        maxResponseBytes: this.maxResponseBytes,
        addressPolicy: this.baseUrl === defaultBaseUrl ? 'publicHTTPS' : 'privateNetworkHTTP',
        timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        const status = error.status ?? 0;
        if (status === 408 || status === 429 || status >= 500) {
          const retryAfter = error.headers?.['retry-after'];
          return newQueryRetry(failure(operation, status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', 'Hacker News is temporarily unavailable'), parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '', this.now()));
        }
        if (status === 404 && operation === 'getItem') return failed(operation, 'notFound', zero, 'NOT_FOUND', 'Hacker News resource was not found');
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
    if (status === 404 && operation === 'getItem') return failed(operation, 'notFound', zero, 'NOT_FOUND', 'Hacker News resource was not found');
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

function parseProviderJSON(body: Uint8Array): unknown {
  const exactJSON = JSON as typeof JSON & { rawJSON(source: string): { rawJSON: string } };
  return exactJSON.parse(Buffer.from(body).toString('utf8'), (_key, value: unknown, context?: { source?: string }) =>
    typeof value === 'number' && context?.source ? exactJSON.rawJSON(context.source) : value) as unknown;
}

function strictInteger(value: unknown, name: string): number {
  const source = value && typeof value === 'object' && 'rawJSON' in value ? String(value.rawJSON) : '';
  if (!/^-?(?:0|[1-9]\d*)$/.test(source)) throw new TypeError(name);
  const integer = Number(source);
  if (!Number.isSafeInteger(integer)) throw new TypeError(name);
  return integer;
}

function fieldError(error: unknown): string { return error instanceof Error ? error.message : 'unknown field'; }

function goTimestamp(time: Date): string {
  return time.toISOString().replace(/\.000Z$/, 'Z').replace(/(\.\d*?[1-9])0+Z$/, '$1Z');
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'hacker-news', operation, message }; }
function failed<Output>(operation: string, branch: string, zero: Output, kind: Failure['kind'], message: string): QueryAttempt<Output> {
  return newQueryBranch(branch, zero, {}, failure(operation, kind, message));
}
