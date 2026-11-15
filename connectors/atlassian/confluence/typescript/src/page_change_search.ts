// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Call, TriggerContext } from '@superdurable/connectors-sdk';
import { MaxChangesPerPage, markRateLimited, waitForPollRequest } from '@superdurable/connectors-sdk/pollingtrigger';
import type { ConfluenceClient, ConfluenceSession } from './client.js';
import type { PageSummary } from './generated/models.js';
import { GoInstant, parseGoTimeLayout } from './go_time.js';
import { readNextCursor } from './go_url.js';
import { webURL } from './operations.js';
import { spaceKeyPattern } from './page_changed.js';

/** The Trigger identity named in failures. */
export const pageChangedTriggerId = 'pageChanged';
/** Confluence's largest content search page; it may return fewer. */
export const pageChangeSearchPageSize = 100;
const contentIdPattern = /^[1-9][0-9]{0,18}$/;
const jsonNumberPattern = /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$/;
const goZeroTime = '0001-01-01T00:00:00Z';

/** Confluence answered HTTP 400 for a stored search cursor, such as an expired one. */
export class SearchCursorRejectedError extends Error {}

/** One page version of a search page: its summary and its exact Go instant. */
export interface ChangedPage { summary: PageSummary; lastModifiedAt: GoInstant }

/** One decoded search page, newest modification first. */
export interface SearchPageResult { pages: ChangedPage[]; searchCursor: string }

interface DecodedResource { id: string; type: string; status: string; title: string; webui: string; space?: [string, string]; version?: [number, string] }

/** A JSON number literal kept as its source text, as Go's json.Number and int decoding see it. */
class NumberLiteral { constructor(readonly text: string) {} }

/** Resolve the credential and site for one poll; a 429 from the site lookup pauses the connection. */
export async function startTriggerSession(client: ConfluenceClient, context: TriggerContext, call: Call): Promise<ConfluenceSession> {
  if (client.needsSiteLookup()) await waitForPollRequest(context);
  const started = await client.startSession(call, pageChangedTriggerId);
  if ('session' in started) return started.session;
  const { failure, retryAfterMs } = started.error;
  if (failure.kind === 'RATE_LIMIT') throw markRateLimited(new Error(failure.message), retryAfterMs);
  throw new Error(failure.kind === 'AUTHENTICATION' ? 'Confluence Trigger credentials are unavailable; reconnect the connection' : failure.message);
}

/** Read one CQL content search page; a 429, or a 503 with Retry-After, pauses the connection. */
export async function searchPageChanges(client: ConfluenceClient, context: TriggerContext, session: ConfluenceSession, cql: string, searchCursor: string): Promise<SearchPageResult> {
  await waitForPollRequest(context);
  const query: Record<string, string> = { cql, limit: String(pageChangeSearchPageSize), expand: 'space,version' };
  if (searchCursor) query.cursor = searchCursor;
  const response = await client.exchange(session, 'GET', 'search', '/content/search', query);
  if (response.error) throw new Error('Confluence page search is temporarily unavailable');
  const retryAfterMs = client.retryAfter(response);
  if (response.status === 429) throw markRateLimited(new Error('Confluence rate limited the page search (HTTP 429)'), retryAfterMs);
  if (response.status === 503 && retryAfterMs > 0) throw markRateLimited(new Error('Confluence page search is unavailable (HTTP 503)'), retryAfterMs);
  if (response.status === 400 && searchCursor) throw new SearchCursorRejectedError('Confluence rejected the stored search cursor');
  if (response.status < 200 || response.status >= 300) throw new Error(`Confluence rejected the page search with HTTP ${response.status}`);
  if (response.isTooLarge) throw new Error('Confluence page search response exceeds maxResponseBytes');
  if (response.reflectsSecret) throw new Error('Confluence page search response contains credential material');
  return decodePageChangeSearch(response.body);
}

/** Keep pages only and require each page's version and time, which order the scan; throws for an invalid page. */
export function decodePageChangeSearch(body: Uint8Array): SearchPageResult {
  let decoded: DecodedResource[], base: string, nextLink: string;
  try {
    const document = JSON.parse(new TextDecoder().decode(body), (_key, value: unknown, context?: { source?: string }) =>
      typeof value === 'number' ? new NumberLiteral(context?.source ?? String(value)) : value);
    const page = optionalObject(document);
    const resources = page.results;
    if (resources !== undefined && resources !== null && !Array.isArray(resources)) throw new TypeError('results is not a list');
    const links = optionalObject(page._links);
    base = stringValue(links.base);
    nextLink = stringValue(links.next);
    decoded = ((resources as unknown[] | null | undefined) ?? []).map(decodeResource);
  } catch {
    throw new Error('Confluence returned an invalid search page');
  }
  if (decoded.length > MaxChangesPerPage) throw new Error(`Confluence search page has ${decoded.length} results, more than ${MaxChangesPerPage}`);
  let searchCursor: string;
  try { searchCursor = readNextCursor(nextLink); }
  catch { throw new Error('Confluence returned an invalid search cursor'); }
  const result: SearchPageResult = { pages: [], searchCursor };
  for (const resource of decoded) {
    if (resource.type !== 'page') continue;
    if (!contentIdPattern.test(resource.id) || !resource.title) throw new Error('Confluence returned an invalid page in the search results: page ID or title is missing');
    const changed = changedPage(resource, base);
    if ((changed.summary.versionNumber ?? 0) < 1 || changed.lastModifiedAt.isZero()) throw new Error(`Confluence search result for page ${changed.summary.id} has no version number or time`);
    result.pages.push(changed);
  }
  return result;
}

function decodeResource(resource: unknown): DecodedResource {
  const value = optionalObject(resource);
  const decoded: DecodedResource = {
    id: stringValue(value.id), type: stringValue(value.type), status: stringValue(value.status), title: stringValue(value.title),
    webui: stringValue(optionalObject(value._links).webui),
  };
  if (value.space !== undefined && value.space !== null) {
    const space = optionalObject(value.space);
    decoded.space = [jsonNumberText(space.id), stringValue(space.key)];
  }
  if (value.version !== undefined && value.version !== null) {
    const version = optionalObject(value.version);
    decoded.version = [integerValue(version.number), stringValue(version.when)];
  }
  return decoded;
}

function changedPage(resource: DecodedResource, base: string): ChangedPage {
  const [spaceIdText, spaceKeyText] = resource.space ?? ['', ''];
  const [versionNumber, when] = resource.version ?? [0, ''];
  // An unparseable time is zero, which the caller rejects.
  const lastModifiedAt = resource.version ? parseGoTimeLayout(when) ?? GoInstant.zero : GoInstant.zero;
  const spaceId = contentIdPattern.test(spaceIdText) ? spaceIdText : '';
  const spaceKey = spaceKeyPattern.test(spaceKeyText) ? spaceKeyText : '';
  const webUrl = webURL(base, resource.webui);
  const summary: PageSummary = {
    id: resource.id, title: resource.title,
    ...(resource.status ? { status: resource.status } : {}),
    ...(spaceId ? { spaceId } : {}), ...(spaceKey ? { spaceKey } : {}),
    ...(versionNumber ? { versionNumber } : {}),
    lastModifiedAt: lastModifiedAt.isZero() ? goZeroTime : lastModifiedAt.format(),
    ...(webUrl ? { webUrl } : {}),
  };
  return { summary, lastModifiedAt };
}

function optionalObject(value: unknown): Record<string, unknown> {
  if (value === undefined || value === null) return {};
  if (typeof value !== 'object' || Array.isArray(value) || value instanceof NumberLiteral) throw new TypeError('value is not an object');
  return value as Record<string, unknown>;
}
function stringValue(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw new TypeError('value is not a string');
  return value;
}
function integerValue(value: unknown): number {
  if (value === undefined || value === null) return 0;
  if (!(value instanceof NumberLiteral) || !/^-?\d+$/.test(value.text)) throw new TypeError('version number is not an integer');
  const integer = BigInt(value.text);
  if (integer < -(2n ** 63n) || integer >= 2n ** 63n) throw new TypeError('version number is out of range');
  return Number(integer);
}
function jsonNumberText(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (value instanceof NumberLiteral) return value.text;
  if (typeof value === 'string' && jsonNumberPattern.test(value)) return value;
  throw new TypeError('space ID is not a number');
}
