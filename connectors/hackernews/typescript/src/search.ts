// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, type Call, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { Parser } from 'htmlparser2';
import type { HackerNewsClient } from './client.js';
import { SearchItemsInputSchema } from './generated/models.js';
import { SearchItemsDefinition, SearchStoriesDefinition } from './generated/definitions.js';
import type { ItemSearchHit, ItemSearchPage, SearchItemsInput, SearchStoriesInput, StorySearchHit, StorySearchPage } from './generated/models.js';
import { failed, fieldError, goTimestamp, parseProviderJSON, strictInteger } from './provider-response.js';

/** Maximum Unicode code points kept in StorySearchHit.storyText. */
export const MaxStoryTextCharacters = 2000;

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

/** Search stories through the Hacker News Search API with one HTTP request. */
export class SearchStoriesOperation implements Query<SearchStoriesInput, StorySearchPage> {
  /** Bind the owning client. */ constructor(private readonly client: HackerNewsClient) {}
  /** Return manifest operation metadata. */ definition() { return SearchStoriesDefinition; }
  /** Validate the search, then decode one result page. Invalid input selects defect before a request is sent. */
  invoke(call: Call, input: SearchStoriesInput): Promise<QueryAttempt<StorySearchPage>> {
    let request: SearchRequest;
    try { request = resolveStorySearchRequest(input); } catch (error) {
      return Promise.resolve(failed('searchStories', 'defect', zeroStorySearchPage, 'VALIDATION', fieldError(error)));
    }
    return this.client.getSearchJSON(call, 'searchStories', `${request.path}?${request.parameters}`, zeroStorySearchPage, body => decodeStorySearchPage(body, request));
  }
}

/** Validated endpoint, encoded filters, and page bounds for a Search API query. */
interface SearchRequest { path: string; parameters: string; page: number; hitsPerPage: number }

/** Search API hit fields that searchStories returns; JSON null decodes as zero, as in Go. */
interface StorySearchResponseHit { objectID: string; title: string; url: string; author: string; points: number; commentCount: number; createdAt: number; storyHTML: string; tags: string[] }

function resolveStorySearchRequest(input: SearchStoriesInput): SearchRequest {
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
  return { path, parameters: encodeSearchParameters(parameters), page, hitsPerPage };
}

/** Decode a Go time.Time JSON string to Unix seconds and nanoseconds; undefined and the zero time are unset. */
function goTimeInstant(value: string | undefined): { seconds: number; nanoseconds: number } | undefined {
  if (value === undefined) return undefined;
  if (!SearchItemsInputSchema.safeParse({ createdAfter: value }).success) throw new TypeError('creation time bounds must be RFC 3339 timestamps');
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

function decodeStorySearchPage(body: Uint8Array, request: SearchRequest): QueryAttempt<StorySearchPage> {
  let response: { hits: StorySearchResponseHit[]; page: number; pages: number; totalHits: number } | undefined;
  try { response = decodeSearchResponse(parseProviderJSON(body), decodeStorySearchResponseHit); } catch { response = undefined; }
  if (!response) return failed('searchStories', 'invalidResponse', zeroStorySearchPage, 'PROTOCOL', 'Hacker News Search returned a malformed result page');
  if (response.page !== request.page || response.hits.length > request.hitsPerPage || response.pages < 0 || response.totalHits < 0) {
    return failed('searchStories', 'invalidResponse', zeroStorySearchPage, 'PROTOCOL', 'Hacker News Search returned an inconsistent result page');
  }
  const stories: StorySearchHit[] = [];
  for (const hit of response.hits) {
    // Go strconv.ParseInt accepts an optional sign and leading zeros in base 10.
    const id = /^[+-]?\d+$/.test(hit.objectID) ? Number(hit.objectID) : Number.NaN;
    if (!Number.isSafeInteger(id) || id <= 0) return failed('searchStories', 'invalidResponse', zeroStorySearchPage, 'PROTOCOL', 'Hacker News Search returned an invalid story ID');
    if (hit.createdAt <= 0 || hit.createdAt > 253402300799) return failed('searchStories', 'invalidResponse', zeroStorySearchPage, 'PROTOCOL', 'Hacker News Search returned an invalid creation time');
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
function decodeSearchResponse<Hit>(value: unknown, readHit: (value: unknown) => Hit): { hits: Hit[]; page: number; pages: number; totalHits: number } | undefined {
  const document = searchFields(value);
  if (!Array.isArray(document.hits)) return undefined;
  return {
    hits: document.hits.map(readHit),
    page: strictInteger(document.page, 'page'), pages: strictInteger(document.nbpages, 'nbPages'),
    totalHits: strictInteger(document.nbhits, 'nbHits'),
  };
}

function searchFields(value: unknown): Record<string, unknown> {
  if (value === null) return {};
  if (typeof value !== 'object' || Array.isArray(value)) throw new TypeError('expected a search record');
  return Object.fromEntries(Object.entries(value).map(([key, field]) => [key.toLowerCase(), field]));
}

function searchString(fields: Record<string, unknown>, name: string): string {
  const value = fields[name];
  if (value == null) return '';
  if (typeof value !== 'string') throw new TypeError(name);
  return value;
}

function searchInteger(fields: Record<string, unknown>, name: string): number {
  return fields[name] == null ? 0 : strictInteger(fields[name], name);
}

function searchTags(value: unknown): string[] {
  if (value == null) return [];
  if (!Array.isArray(value)) throw new TypeError('_tags');
  return value.map(tag => {
    if (tag === null) return '';
    if (typeof tag !== 'string') throw new TypeError('_tags');
    return tag;
  });
}

function decodeStorySearchResponseHit(value: unknown): StorySearchResponseHit {
  const fields = searchFields(value);
  return {
    objectID: searchString(fields, 'objectid'), title: searchString(fields, 'title'), url: searchString(fields, 'url'),
    author: searchString(fields, 'author'), points: searchInteger(fields, 'points'), commentCount: searchInteger(fields, 'num_comments'),
    createdAt: searchInteger(fields, 'created_at_i'), storyHTML: searchString(fields, 'story_text'), tags: searchTags(fields._tags),
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


// Go QueryEscape preserves tilde and escapes asterisk, unlike URLSearchParams.
function encodeSearchParameters(parameters: URLSearchParams): string {
  parameters.sort();
  return parameters.toString().replaceAll('%7E', '~').replaceAll('*', '%2A');
}

const zeroItemSearchPage: ItemSearchPage = { items: [], page: 0, pages: 0, totalHits: 0, hitsPerPage: 0, nextPage: 0 };
const indexedItemTypes = new Set(['story', 'comment', 'poll', 'pollopt']);

/** Search public indexed stories, comments, polls, and poll options. */
export class SearchItemsOperation implements Query<SearchItemsInput, ItemSearchPage> {
  /** Bind the owning client's Search API endpoint and response limits. */
  constructor(private readonly client: HackerNewsClient) {}
  /** Return the manifest Query contract. */
  definition() { return SearchItemsDefinition; }
  /** Validate the filters locally and read one page from the selected index. */
  invoke(call: Call, input: SearchItemsInput): Promise<QueryAttempt<ItemSearchPage>> {
    let request: SearchRequest;
    try { request = resolveItemSearchRequest(input); } catch (error) {
      return Promise.resolve(failed('searchItems', 'defect', zeroItemSearchPage, 'VALIDATION', fieldError(error)));
    }
    return this.client.getSearchJSON(call, 'searchItems', `${request.path}?${request.parameters}`, zeroItemSearchPage,
      body => decodeItemSearchPage(body, request, input.type));
  }
}

function resolveItemSearchRequest(input: SearchItemsInput): SearchRequest {
  const query = input.query ?? '';
  const page = input.page ?? 0;
  const hitsPerPage = input.hitsPerPage || defaultStorySearchHitsPerPage;
  if (typeof query !== 'string' || Buffer.byteLength(query) > maxSearchQueryBytes || !Number.isSafeInteger(page) || page < 0) {
    throw new TypeError('Hacker News item search query or page is invalid');
  }
  if (!Number.isSafeInteger(hitsPerPage) || hitsPerPage < 1 || hitsPerPage > maxStorySearchHitsPerPage || page * hitsPerPage >= reachableStorySearchHits) {
    throw new TypeError('Hacker News item search page size is invalid');
  }
  if (input.type && !indexedItemTypes.has(input.type)) throw new TypeError('Hacker News item type is invalid');
  if (input.sortBy && input.sortBy !== 'relevance' && input.sortBy !== 'date') throw new TypeError('Hacker News item search sort is invalid');
  const parameters = new URLSearchParams({ page: String(page), hitsPerPage: String(hitsPerPage) });
  if (input.type) parameters.set('tags', input.type);
  if (query) parameters.set('query', query);
  const after = goTimeInstant(input.createdAfter);
  if (after) parameters.set('numericFilters', `created_at_i>${after.seconds}`);
  return { path: input.sortBy === 'date' ? '/search_by_date' : '/search', parameters: encodeSearchParameters(parameters), page, hitsPerPage };
}

function readIndexedItem(value: unknown) {
  const fields = searchFields(value);
  return {
    objectID: searchString(fields, 'objectid'), title: searchString(fields, 'title'), storyTitle: searchString(fields, 'story_title'),
    url: searchString(fields, 'url'), author: searchString(fields, 'author'), storyHTML: searchString(fields, 'story_text'),
    commentHTML: searchString(fields, 'comment_text'), createdAt: searchInteger(fields, 'created_at_i'),
    parentID: searchInteger(fields, 'parent_id'), storyID: searchInteger(fields, 'story_id'), tags: searchTags(fields._tags),
  };
}

function decodeItemSearchPage(body: Uint8Array, request: SearchRequest, type: string | undefined): QueryAttempt<ItemSearchPage> {
  const invalid = (message: string) => failed('searchItems', 'invalidResponse', zeroItemSearchPage, 'PROTOCOL', message);
  let result;
  try { result = decodeSearchResponse(parseProviderJSON(body), readIndexedItem); } catch { result = undefined; }
  if (!result) return invalid('Hacker News Search returned malformed items');
  if (result.page !== request.page || result.pages < 0 || result.totalHits < 0 || result.hits.length > request.hitsPerPage) {
    return invalid('Hacker News Search returned inconsistent item paging');
  }
  const items: ItemSearchHit[] = [];
  for (const hit of result.hits) {
    const id = /^[+-]?\d+$/.test(hit.objectID) ? Number(hit.objectID) : NaN;
    if (!Number.isSafeInteger(id) || id <= 0 || hit.createdAt <= 0 || hit.createdAt > 253402300799) return invalid('Hacker News Search returned an invalid item');
    const itemType = hit.tags.find(tag => indexedItemTypes.has(tag));
    if (!itemType || (type && itemType !== type)) return invalid('Hacker News Search returned an unexpected item type');
    const item: ItemSearchHit = { id, type: itemType, createdAt: goTimestamp(new Date(hit.createdAt * 1000)), discussionUrl: `https://news.ycombinator.com/item?id=${id}` };
    const text = Array.from(plainTextFromStoryHTML(itemType === 'comment' ? hit.commentHTML : hit.storyHTML)).slice(0, MaxStoryTextCharacters).join('');
    if (text) item.text = text;
    if (hit.title || hit.storyTitle) item.title = hit.title || hit.storyTitle;
    if (hit.url) item.url = hit.url;
    if (hit.author) item.author = hit.author;
    if (hit.parentID) item.parentId = hit.parentID;
    if (hit.storyID) item.storyId = hit.storyID;
    items.push(item);
  }
  return newQueryBranch('searched', {
    items, page: result.page, pages: result.pages, totalHits: result.totalHits, hitsPerPage: request.hitsPerPage,
    nextPage: request.page + 1 < result.pages ? request.page + 1 : 0,
  }, {});
}
