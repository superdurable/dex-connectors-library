// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { Parser } from 'htmlparser2';
import { GetItemDefinition, ListStoryIDsDefinition, SearchStoriesDefinition } from './generated/definitions.js';
import type { Config, GetItemInput, Item, ListStoryIDsInput, SearchStoriesInput, StoryIDs, StorySearchHit, StorySearchPage } from './generated/models.js';

/** Maximum Unicode code points kept in StorySearchHit.storyText. */
export const MaxStoryTextCharacters = 2000;

const defaultBaseUrl = 'https://hacker-news.firebaseio.com/v0';
const defaultSearchBaseUrl = 'https://hn.algolia.com/api/v1';
const zeroTime = '0001-01-01T00:00:00Z';
const zeroIds: StoryIDs = { ids: [], hasMore: false, observedAt: zeroTime };
const zeroItem: Item = { id: 0, type: '', discussionUrl: '' };
const validFeeds = new Set(['top', 'best', 'new', 'ask', 'show']);
const validItemTypes = new Set(['story', 'comment', 'job', 'poll', 'pollopt']);
const zeroStorySearchPage: StorySearchPage = { stories: [], page: 0, pages: 0, totalHits: 0, hitsPerPage: 0, nextPage: 0 };
// Accepted tags in StorySearchHit.tags order, independent of the provider's order.
const knownStoryTags = ['story', 'show_hn', 'ask_hn', 'front_page'] as const;
// The Search API rejects longer queries with HTTP 400.
const maxSearchQueryBytes = 512;
const maxStorySearchHitsPerPage = 100;
const defaultStorySearchHitsPerPage = 20;
// The Search API serves only this many leading hits of one query across pages.
const reachableStorySearchHits = 1000;
const paragraphTags = new Set(['p', 'pre', 'br', 'div', 'li', 'blockquote']);
// Go unicode.IsSpace, which strings.Fields splits on; JavaScript \s differs at U+0085 and U+FEFF.
const goWhitespace = /[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+/u;
const goTimePattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{1,2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|([+-])(\d{2}):(\d{2}))$/;
const goZeroTimeSeconds = -62_135_596_800;

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

/** Search stories through the Hacker News Search API with one HTTP request. */
export class SearchStoriesOperation implements Query<SearchStoriesInput, StorySearchPage> {
  /** Bind the owning client. */ constructor(private readonly client: HackerNewsClient) {}
  /** Return manifest operation metadata. */ definition() { return SearchStoriesDefinition; }
  /** Validate the search, then decode one result page. Invalid input selects defect before a request is sent. */
  invoke(call: Call, input: SearchStoriesInput): Promise<QueryAttempt<StorySearchPage>> {
    let request: StorySearchRequest;
    try { request = resolveStorySearchRequest(input); } catch (error) {
      return Promise.resolve(failed('searchStories', 'defect', zeroStorySearchPage, 'VALIDATION', fieldError(error)));
    }
    return this.client.getSearchJSON(call, 'searchStories', `${request.path}?${request.parameters}`, zeroStorySearchPage, body => decodeStorySearchPage(body, request));
  }
}

/** A validated SearchStoriesInput resolved to its Search API request. */
interface StorySearchRequest { path: string; parameters: string; page: number; hitsPerPage: number }

/** Search API hit fields that searchStories returns; JSON null decodes as zero, as in Go. */
interface StorySearchResponseHit { objectID: string; title: string; url: string; author: string; points: number; commentCount: number; createdAt: number; storyHTML: string; tags: string[] }

function resolveStorySearchRequest(input: SearchStoriesInput): StorySearchRequest {
  let path: string;
  switch (input.sortBy ?? '') {
    case '': case 'relevance': path = '/search'; break;
    case 'date': path = '/search_by_date'; break;
    default: throw new TypeError('sortBy must be relevance or date');
  }
  const query = input.query ?? '';
  if (Buffer.byteLength(query, 'utf8') > maxSearchQueryBytes) throw new TypeError(`query must be at most ${maxSearchQueryBytes} bytes`);
  const tags: string[] = ['story'];
  for (const tag of input.tags ?? []) {
    if (!(knownStoryTags as readonly string[]).includes(tag)) throw new TypeError('tags must be story, show_hn, ask_hn, or front_page');
    if (!tags.includes(tag)) tags.push(tag);
  }
  const minPoints = input.minPoints ?? 0;
  if (!Number.isSafeInteger(minPoints) || minPoints < 0) throw new TypeError('minPoints cannot be negative');
  const createdAfter = goTimeInstant(input.createdAfter);
  const createdBefore = goTimeInstant(input.createdBefore);
  if (createdAfter && createdBefore && !isLaterInstant(createdBefore, createdAfter)) throw new TypeError('createdBefore must be later than createdAfter');
  const hitsPerPage = input.hitsPerPage || defaultStorySearchHitsPerPage;
  if (!Number.isSafeInteger(hitsPerPage) || hitsPerPage < 1 || hitsPerPage > maxStorySearchHitsPerPage) throw new TypeError(`hitsPerPage must be from 1 through ${maxStorySearchHitsPerPage}`);
  const page = input.page ?? 0;
  if (!Number.isSafeInteger(page) || page < 0) throw new TypeError('page cannot be negative');
  if (page >= reachableStorySearchHits || page * hitsPerPage >= reachableStorySearchHits) {
    throw new TypeError(`page times hitsPerPage must be below ${reachableStorySearchHits}, the hits the Search API serves for one query`);
  }
  // Go url.Values.Encode sorts keys; Go omits an empty query.
  const parameters = new URLSearchParams();
  parameters.set('hitsPerPage', String(hitsPerPage));
  const numericFilters: string[] = [];
  if (createdAfter) numericFilters.push(`created_at_i>${createdAfter.seconds}`);
  if (createdBefore) numericFilters.push(`created_at_i<${createdBefore.seconds + (createdBefore.nanoseconds > 0 ? 1 : 0)}`);
  if (minPoints > 0) numericFilters.push(`points>=${minPoints}`);
  if (numericFilters.length) parameters.set('numericFilters', numericFilters.join(','));
  parameters.set('page', String(page));
  if (query) parameters.set('query', query);
  parameters.set('tags', tags.join(','));
  return { path, parameters: parameters.toString(), page, hitsPerPage };
}

/** Decode a Go time.Time JSON string to Unix seconds and nanoseconds; undefined and the zero time are unset. */
function goTimeInstant(value: string | undefined): { seconds: number; nanoseconds: number } | undefined {
  if (value === undefined) return undefined;
  const match = goTimePattern.exec(value);
  if (!match) throw new TypeError('creation time bounds must be RFC 3339 timestamps');
  const [, year, month, day, hour, minute, second, fraction, zone, sign, zoneHour, zoneMinute] = match;
  const offsetSeconds = zone === 'Z' ? 0 : (Number(zoneHour) * 3600 + Number(zoneMinute) * 60) * (sign === '+' ? 1 : -1);
  const seconds = daysFromCivil(Number(year), Number(month), Number(day)) * 86_400 + Number(hour) * 3600 + Number(minute) * 60 + Number(second) - offsetSeconds;
  const nanoseconds = Number((fraction ?? '').slice(0, 9).padEnd(9, '0'));
  return seconds === goZeroTimeSeconds && nanoseconds === 0 ? undefined : { seconds, nanoseconds };
}

function isLaterInstant(later: { seconds: number; nanoseconds: number }, earlier: { seconds: number; nanoseconds: number }): boolean {
  return later.seconds > earlier.seconds || (later.seconds === earlier.seconds && later.nanoseconds > earlier.nanoseconds);
}

// Days since 1970-01-01 in the proleptic Gregorian calendar, valid for every four-digit year.
function daysFromCivil(year: number, month: number, day: number): number {
  const shiftedYear = month <= 2 ? year - 1 : year;
  const era = Math.floor(shiftedYear / 400);
  const yearOfEra = shiftedYear - era * 400;
  const dayOfYear = Math.floor((153 * (month + (month > 2 ? -3 : 9)) + 2) / 5) + day - 1;
  const dayOfEra = yearOfEra * 365 + Math.floor(yearOfEra / 4) - Math.floor(yearOfEra / 100) + dayOfYear;
  return era * 146_097 + dayOfEra - 719_468;
}

function decodeStorySearchPage(body: Uint8Array, request: StorySearchRequest): QueryAttempt<StorySearchPage> {
  let response: { hits: StorySearchResponseHit[]; page: number; pages: number; totalHits: number } | undefined;
  try { response = decodeStorySearchResponse(parseProviderJSON(body)); } catch { response = undefined; }
  if (!response) return failed('searchStories', 'invalidResponse', zeroStorySearchPage, 'PROTOCOL', 'Hacker News Search returned a malformed result page');
  if (response.page !== request.page || response.hits.length > request.hitsPerPage || response.pages < 0 || response.totalHits < 0) {
    return failed('searchStories', 'invalidResponse', zeroStorySearchPage, 'PROTOCOL', 'Hacker News Search returned an inconsistent result page');
  }
  const stories: StorySearchHit[] = [];
  for (const hit of response.hits) {
    // Go strconv.ParseInt accepts an optional sign and leading zeros in base 10.
    const id = /^[+-]?\d+$/.test(hit.objectID) ? Number(hit.objectID) : Number.NaN;
    if (!Number.isSafeInteger(id) || id <= 0) return failed('searchStories', 'invalidResponse', zeroStorySearchPage, 'PROTOCOL', 'Hacker News Search returned an invalid story ID');
    if (hit.createdAt <= 0) return failed('searchStories', 'invalidResponse', zeroStorySearchPage, 'PROTOCOL', 'Hacker News Search returned an invalid creation time');
    const story: StorySearchHit = {
      id, title: hit.title, discussionUrl: `https://news.ycombinator.com/item?id=${id}`, author: hit.author, points: hit.points,
      commentCount: hit.commentCount, createdAt: goTimestamp(new Date(hit.createdAt * 1000)), tags: knownStoryTags.filter(tag => hit.tags.includes(tag)),
    };
    if (hit.url) story.url = hit.url;
    const storyText = Array.from(plainTextFromStoryHTML(hit.storyHTML));
    if (storyText.length) story.storyText = storyText.slice(0, MaxStoryTextCharacters).join('');
    if (storyText.length > MaxStoryTextCharacters) story.isStoryTextTruncated = true;
    stories.push(story);
  }
  return newQueryBranch('searched', {
    stories, page: request.page, pages: response.pages, totalHits: response.totalHits, hitsPerPage: request.hitsPerPage,
    nextPage: request.page + 1 < response.pages ? request.page + 1 : 0,
  }, {});
}

/** Apply Go encoding/json rules to the result page; undefined means Go would report a malformed page. */
function decodeStorySearchResponse(value: unknown): { hits: StorySearchResponseHit[]; page: number; pages: number; totalHits: number } | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined;
  const document = value as Record<string, unknown>;
  if (!Array.isArray(document.hits) || [document.page, document.nbPages, document.nbHits].some(field => field === undefined || field === null)) return undefined;
  const hits = document.hits.map(decodeStorySearchResponseHit);
  return { hits, page: strictInteger(document.page, 'page'), pages: strictInteger(document.nbPages, 'nbPages'), totalHits: strictInteger(document.nbHits, 'nbHits') };
}

function decodeStorySearchResponseHit(value: unknown): StorySearchResponseHit {
  if (value !== null && (typeof value !== 'object' || Array.isArray(value))) throw new TypeError('hit must be an object');
  const document = (value ?? {}) as Record<string, unknown>;
  const string = (name: string) => { const field = document[name]; if (field === undefined || field === null) return ''; if (typeof field !== 'string') throw new TypeError(name); return field; };
  const integer = (name: string) => { const field = document[name]; if (field === undefined || field === null) return 0; return strictInteger(field, name); };
  const tags = document._tags;
  if (tags !== undefined && tags !== null && !Array.isArray(tags)) throw new TypeError('_tags');
  const tagNames = Array.isArray(tags) ? tags.map(tag => { if (tag === null) return ''; if (typeof tag !== 'string') throw new TypeError('_tags'); return tag; }) : [];
  return {
    objectID: string('objectID'), title: string('title'), url: string('url'), author: string('author'), points: integer('points'),
    commentCount: integer('num_comments'), createdAt: integer('created_at_i'), storyHTML: string('story_text'), tags: tagNames,
  };
}

/** Keep the visible text of Hacker News story HTML, with a blank line between paragraphs. */
function plainTextFromStoryHTML(storyHTML: string): string {
  if (!storyHTML) return '';
  const text = new StoryTextBuilder();
  const parser = new Parser({
    ontext(value) { text.writeText(value); },
    onopentag(name) { if (paragraphTags.has(name)) text.endParagraph(); },
    onclosetag(name) { if (paragraphTags.has(name)) text.endParagraph(); },
  }, { decodeEntities: true });
  parser.write(storyHTML);
  parser.end();
  text.endParagraph();
  return text.paragraphs.join('\n\n');
}

/** Collects paragraphs; Hacker News separates them with <p> and seldom closes it. */
class StoryTextBuilder {
  readonly paragraphs: string[] = [];
  private current = '';
  /** Append decoded text to the current paragraph. */
  writeText(value: string): void { this.current += value; }
  /** Collapse whitespace runs in the current paragraph and keep it when it has text. */
  endParagraph(): void {
    const paragraph = this.current.split(goWhitespace).filter(Boolean).join(' ');
    this.current = '';
    if (paragraph) this.paragraphs.push(paragraph);
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
