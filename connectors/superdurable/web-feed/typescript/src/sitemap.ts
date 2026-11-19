// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { decodeXMLDocument, type FetchFailure, type FetchedDocument, invalidResponse } from './document.js';
import { parseGoURL } from './go_url.js';
import type { ChildSitemap, Sitemap, SitemapEntry } from './generated/models.js';
import { boundedText, goTrimSpace, maxTitleCharacters, maxURLBytes, resolveLink } from './text.js';
import { compareInstants, type Instant, parseInputTimestamp, parseLooseTimestamp, formatInstant } from './timestamp.js';
import { readRootElement, type XMLName, type XMLTokenizer } from './xml.js';

const maxSitemapScanEntries = 100_000;
const maxSitemapElementDepth = 8;
const namespaceGoogleNews = 'http://www.google.com/schemas/sitemap-news/0.9';

/** The validated selection of one readSitemap call. */
export interface SitemapFilter { pathPrefix: string; lastModifiedAfter: Instant | undefined; limit: number }

interface SitemapRecord { location: string; lastModified: string; newsTitle: string; newsPublication: string }
interface DatedEntry<Value> { value: Value; time: Instant | undefined }

/** Stream the document so a large urlset never becomes one element tree. */
export function parseSitemapDocument(sitemap: Sitemap, document: FetchedDocument, filter: SitemapFilter): FetchFailure | undefined {
  let start = 0;
  const body = document.body;
  if (body[0] === 0xef && body[1] === 0xbb && body[2] === 0xbf) start = 3;
  while (start < body.length && [0x20, 0x09, 0x0d, 0x0a].includes(body[start]!)) start++;
  let tokenizer: XMLTokenizer;
  let entryName: string;
  try {
    tokenizer = decodeXMLDocument(body.subarray(start), document.charsetLabel);
    const root = readRootElement(tokenizer);
    if (root.name.local === 'urlset') { sitemap.kind = 'urlset'; entryName = 'url'; }
    else if (root.name.local === 'sitemapindex') { sitemap.kind = 'sitemapIndex'; entryName = 'sitemap'; }
    else if (root.name.local.toLowerCase() === 'html') return invalidResponse('the URL serves an HTML page, not a sitemap');
    else return invalidResponse('the XML root element is neither urlset nor sitemapindex');
  } catch { return invalidResponse('the document is not XML'); }
  const base = document.finalURL;
  const entries: Array<DatedEntry<SitemapEntry>> = [];
  const children: Array<DatedEntry<ChildSitemap>> = [];
  let scanned = 0;
  let totalEntryCount = 0;
  try {
    while (true) {
      const token = tokenizer.next();
      if (!token) break;
      if (token.kind !== 'start') continue;
      if (token.name.local !== entryName) { skipElement(tokenizer); continue; }
      let record: SitemapRecord;
      try { record = readSitemapRecord(tokenizer); } catch { return invalidResponse('the sitemap XML is malformed or nested too deeply'); }
      if (++scanned > maxSitemapScanEntries) return invalidResponse('the sitemap lists more than 100000 entries');
      const location = resolveLink(base, record.location);
      if (location === '') continue;
      const lastModified = parseLooseTimestamp(record.lastModified);
      if (sitemap.kind === 'sitemapIndex') {
        const child: ChildSitemap = { url: location };
        if (lastModified) child.lastModifiedAt = formatInstant(lastModified);
        children.push({ value: child, time: lastModified });
        continue;
      }
      totalEntryCount++;
      const newsPublished = parseLooseTimestamp(record.newsPublication);
      const entry: SitemapEntry = { url: location };
      if (lastModified) entry.lastModifiedAt = formatInstant(lastModified);
      const newsTitle = boundedText(record.newsTitle, maxTitleCharacters);
      if (newsTitle) entry.newsTitle = newsTitle;
      if (newsPublished) entry.newsPublishedAt = formatInstant(newsPublished);
      const effective = lastModified ?? newsPublished;
      if (matchesFilter(filter, location, effective)) entries.push({ value: entry, time: effective });
    }
  } catch { return invalidResponse('the sitemap XML is malformed'); }
  if (sitemap.kind === 'sitemapIndex') {
    sitemap.totalEntryCount = children.length;
    sitemap.matchedEntryCount = children.length;
    sitemap.sitemaps = newestFirst(children).slice(0, filter.limit);
    sitemap.hasMore = children.length > filter.limit;
    return undefined;
  }
  sitemap.totalEntryCount = totalEntryCount;
  sitemap.matchedEntryCount = entries.length;
  sitemap.entries = newestFirst(entries).slice(0, filter.limit);
  sitemap.hasMore = entries.length > filter.limit;
  return undefined;
}

/** Validate the path prefix, date filter, and limit of one call. */
export function newSitemapFilter(pathPrefix: string | undefined, lastModifiedAfter: string | null | undefined, limit: number | undefined): SitemapFilter {
  const filter: SitemapFilter = { pathPrefix: pathPrefix ?? '', lastModifiedAfter: undefined, limit: limit || 200 };
  if (!Number.isSafeInteger(filter.limit) || filter.limit < 1 || filter.limit > 2000) throw new TypeError('limit must be from 1 through 2000');
  if (filter.pathPrefix !== '' && !filter.pathPrefix.startsWith('/')) throw new TypeError('pathPrefix must start with a slash, such as /blog/');
  if (Buffer.byteLength(filter.pathPrefix, 'utf8') > 512 || /[?#]/.test(filter.pathPrefix)) throw new TypeError('pathPrefix must be a URL path of at most 512 bytes without a query or fragment');
  if (lastModifiedAfter) {
    filter.lastModifiedAfter = parseInputTimestamp(lastModifiedAfter);
    if (!filter.lastModifiedAfter) throw new TypeError('lastModifiedAfter must be an RFC 3339 time');
  }
  return filter;
}

// Read loc, lastmod, and Google News fields of the entry the tokenizer just opened.
function readSitemapRecord(tokenizer: XMLTokenizer): SitemapRecord {
  const record: SitemapRecord = { location: '', lastModified: '', newsTitle: '', newsPublication: '' };
  const path: XMLName[] = [];
  let text = '';
  while (true) {
    const token = tokenizer.next();
    if (!token) throw new Error('unexpected EOF');
    if (token.kind === 'start') {
      if (path.length === maxSitemapElementDepth) throw new Error('sitemap entry is nested too deeply');
      path.push(token.name);
      text = '';
    } else if (token.kind === 'text') {
      if (Buffer.byteLength(text, 'utf8') < maxURLBytes * 2) text += token.text;
    } else {
      if (path.length === 0) return record;
      assignRecordField(record, path, goTrimSpace(text));
      path.pop();
      text = '';
    }
  }
}

function assignRecordField(record: SitemapRecord, path: XMLName[], value: string): void {
  const leaf = path[path.length - 1]!;
  if (path.length === 1 && leaf.local === 'loc' && record.location === '') record.location = value;
  else if (path.length === 1 && leaf.local === 'lastmod' && record.lastModified === '') record.lastModified = value;
  else if (leaf.space === namespaceGoogleNews && leaf.local === 'title' && record.newsTitle === '') record.newsTitle = value;
  else if (leaf.space === namespaceGoogleNews && leaf.local === 'publication_date' && record.newsPublication === '') record.newsPublication = value;
}

function skipElement(tokenizer: XMLTokenizer): void {
  let depth = 1;
  while (depth > 0) {
    const token = tokenizer.next();
    if (!token) throw new Error('unexpected EOF');
    if (token.kind === 'start') depth++;
    else if (token.kind === 'end') depth--;
  }
}

function matchesFilter(filter: SitemapFilter, location: string, effective: Instant | undefined): boolean {
  if (filter.pathPrefix !== '') {
    let path: string;
    try { path = parseGoURL(location).path; } catch { return false; }
    if (!path.startsWith(filter.pathPrefix)) return false;
  }
  if (filter.lastModifiedAfter) return effective !== undefined && compareInstants(effective, filter.lastModifiedAfter) > 0;
  return true;
}

// Dated values by time descending, then undated values in document order; the sort is stable.
function newestFirst<Value>(values: Array<DatedEntry<Value>>): Value[] {
  return [...values].sort((left, right) => {
    if (!left.time) return right.time ? 1 : 0;
    if (!right.time) return -1;
    return compareInstants(right.time, left.time);
  }).map(entry => entry.value);
}

/** Accept the XML, gzip, binary, and text media types sites serve sitemaps with. */
export function isSitemapMediaType(mediaType: string): boolean {
  if (['', 'application/xml', 'application/gzip', 'application/x-gzip', 'application/octet-stream'].includes(mediaType)) return true;
  if (mediaType.startsWith('text/')) return mediaType !== 'text/css' && mediaType !== 'text/javascript';
  return mediaType.startsWith('application/') && mediaType.endsWith('+xml');
}
