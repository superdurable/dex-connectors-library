// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import {
  defectFailure, fetchDocument, type DocumentRequest, type FetchFailure, type FetchSettings, isPrintableHeaderText,
  parseRequestURL, sendPublicHTTPS, validatorsFromInput,
} from './document.js';
import { parseFeedDocument } from './feed.js';
import { DiscoverFeedsDefinition, ProviderID, ReadFeedDefinition, ReadPageMetadataDefinition, ReadSitemapDefinition } from './generated/definitions.js';
import type {
  Config, DiscoverFeedsInput, DiscoveredFeed, DiscoveredFeeds, Feed, PageMetadata, ReadFeedInput, ReadPageMetadataInput,
  ReadSitemapInput, Sitemap, StructuredDataMetadata,
} from './generated/models.js';
import { hasRelation, headBaseURL, isHTMLMediaType, scanHTMLHead } from './html_head.js';
import { isSitemapMediaType, newSitemapFilter, parseSitemapDocument } from './sitemap.js';
import { boundedText, firstNonEmpty, maxDescriptionCharacters, maxNameCharacters, maxTitleCharacters, resolveLink } from './text.js';
import { optionalTimestamp } from './timestamp.js';

/** Default bound on one HTTP request, in milliseconds. */
export const DefaultTimeoutMs = 15_000;
/** Default largest feed or sitemap after decompression, in bytes. */
export const DefaultMaxDocumentBytes = 10_485_760;
/** Default bytes of an HTML page that are read. */
export const DefaultMaxPageBytes = 1_048_576;
/** Default User-Agent header. */
export const DefaultUserAgent = 'Mozilla/5.0 (compatible; DexWebFeed; +https://connectors.superdurable.io)';

const zeroTime = '0001-01-01T00:00:00Z';
const feedAcceptHeader = 'application/feed+json, application/atom+xml, application/rss+xml, application/rdf+xml;q=0.9, application/xml;q=0.8, text/xml;q=0.8, application/json;q=0.7, */*;q=0.5';
const sitemapAcceptHeader = 'application/xml, text/xml;q=0.9, application/gzip;q=0.8, */*;q=0.5';
const pageAcceptHeader = 'text/html, application/xhtml+xml;q=0.9, */*;q=0.1';
const maxDiscoveredFeeds = 20;
const maxStructuredDataNodes = 500;
const maxStructuredDataDepth = 8;
const articleTypes = new Set(['Article', 'BlogPosting', 'NewsArticle', 'TechArticle', 'ScholarlyArticle', 'Report', 'LiveBlogPosting',
  'SocialMediaPosting', 'DiscussionForumPosting', 'AnalysisNewsArticle', 'OpinionNewsArticle', 'ReportageNewsArticle',
  'ReviewNewsArticle', 'BackgroundNewsArticle', 'Posting']);

/** Optional dependencies of a WebFeedClient. */
export interface WebFeedClientOptions {
  /**
   * Sends one request. The default uses the SDK's PublicHTTPS address policy: https only, every resolved
   * address public, and no redirect replay. A replacement must enforce `request.addressPolicy`; tests use one
   * to replay recorded responses.
   */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock for fetchedAt and HTTP-date Retry-After values; defaults to the system clock. */
  now?: () => Date;
}

/**
 * Reads public feeds, sitemaps, and pages for one connection. Every request is a GET over https to a public
 * address; an http URL is requested over https, an IP literal the policy refuses is never requested, and the
 * client follows at most five redirects through the same checks. It sends no cookies or credentials and is
 * safe for concurrent use. Construction validates the configuration without contacting the network.
 */
export class WebFeedClient {
  private readonly timeoutMs: number;
  private readonly maxDocumentBytes: number;
  private readonly maxPageBytes: number;
  private readonly userAgent: string;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate the configuration; omitted fields take the manifest defaults. Throws a TypeError for an out-of-range setting. */
  constructor(config: Config = {}, options: WebFeedClientOptions = {}) {
    this.timeoutMs = config.timeoutMs || DefaultTimeoutMs;
    this.maxDocumentBytes = config.maxDocumentBytes || DefaultMaxDocumentBytes;
    this.maxPageBytes = config.maxPageBytes || DefaultMaxPageBytes;
    this.userAgent = config.userAgent || DefaultUserAgent;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 1000) throw new TypeError('configuration timeout must be at least one second');
    if (!Number.isSafeInteger(this.maxDocumentBytes) || this.maxDocumentBytes < 1024 || this.maxDocumentBytes > 256 << 20) throw new TypeError('configuration maxDocumentBytes must be from 1024 through 268435456');
    if (!Number.isSafeInteger(this.maxPageBytes) || this.maxPageBytes < 1024 || this.maxPageBytes > 64 << 20) throw new TypeError('configuration maxPageBytes must be from 1024 through 67108864');
    if (!isPrintableHeaderText(this.userAgent, 256)) throw new TypeError('configuration userAgent must be 1 to 256 printable ASCII characters');
    this.sendRequest = options.send ?? sendPublicHTTPS;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the readFeed Query. */ readFeed(): ReadFeedOperation { return new ReadFeedOperation(this); }
  /** Bind the discoverFeeds Query. */ discoverFeeds(): DiscoverFeedsOperation { return new DiscoverFeedsOperation(this); }
  /** Bind the readSitemap Query. */ readSitemap(): ReadSitemapOperation { return new ReadSitemapOperation(this); }
  /** Bind the readPageMetadata Query. */ readPageMetadata(): ReadPageMetadataOperation { return new ReadPageMetadataOperation(this); }

  /** Request settings for one call; internal to the operations. */
  settings(call: Call): FetchSettings {
    return { send: this.sendRequest, userAgent: this.userAgent, timeoutMs: this.timeoutMs, now: this.now, signal: call.context?.cancellationSignal };
  }
  /** Bound for feeds and sitemaps. */ documentBytes(): number { return this.maxDocumentBytes; }
  /** Bound for HTML page prefixes. */ pageBytes(): number { return this.maxPageBytes; }
  /** Current time as Go RFC 3339 in UTC. */ fetchedAt(): string { return optionalTimestampFromDate(this.now()); }
}

/** Reads one feed document per invocation, following redirects. */
export class ReadFeedOperation implements Query<ReadFeedInput, Feed> {
  /** Bind the client. */ constructor(private readonly client: WebFeedClient) {}
  /** The generated readFeed contract. */ definition(): QueryDefinition { return ReadFeedDefinition; }
  /** Read and parse one feed; a 304 to a conditional GET selects notModified, and transient failures retry. */
  async invoke(call: Call, input: ReadFeedInput): Promise<QueryAttempt<Feed>> {
    const zero = (): Feed => ({ finalUrl: '', fetchedAt: zeroTime, items: [], itemCount: 0, hasMore: false });
    let request: DocumentRequest;
    const limit = input.limit || 50;
    const maxSummaryCharacters = input.maxSummaryCharacters || 500;
    try {
      const target = parseRequestURL(input.url);
      const validators = validatorsFromInput(input.etag, input.lastModified);
      if (!Number.isSafeInteger(limit) || limit < 1 || limit > 200) throw new TypeError('limit must be from 1 through 200');
      if (!Number.isSafeInteger(maxSummaryCharacters) || maxSummaryCharacters < 1 || maxSummaryCharacters > 4000) throw new TypeError('maxSummaryCharacters must be from 1 through 4000');
      request = { target, accept: feedAcceptHeader, validators, maxBytes: this.client.documentBytes(), isPrefixRead: false, mayBeGzip: true, acceptsMediaType: isFeedMediaType, expectedContent: 'a feed' };
    } catch (error) { return attemptFromFailure('readFeed', defectFailure(error), zero()); }
    const fetched = await fetchDocument(this.client.settings(call), request);
    if ('failure' in fetched) return attemptFromFailure('readFeed', fetched.failure, zero());
    const document = fetched.document;
    const response = withValidators<Feed>({ finalUrl: document.finalURL.toString(), fetchedAt: this.client.fetchedAt(), items: [], itemCount: 0, hasMore: false }, document.validators);
    if (document.isNotModified) return newQueryBranch('notModified', response);
    const parsed = parseFeedDocument(document.body, document.charsetLabel, { limit, maxSummaryCharacters, documentURL: response.finalUrl });
    if ('failure' in parsed) return attemptFromFailure('readFeed', parsed.failure, zero());
    return newQueryBranch('read', withValidators<Feed>({ ...parsed.feed, finalUrl: response.finalUrl, fetchedAt: response.fetchedAt }, document.validators));
  }
}

/** Reads one HTML page per invocation and lists the feeds it advertises. */
export class DiscoverFeedsOperation implements Query<DiscoverFeedsInput, DiscoveredFeeds> {
  /** Bind the client. */ constructor(private readonly client: WebFeedClient) {}
  /** The generated discoverFeeds contract. */ definition(): QueryDefinition { return DiscoverFeedsDefinition; }
  /** Read the page prefix and list its rel=alternate feed links; it never requests the feeds or guesses paths. */
  async invoke(call: Call, input: DiscoverFeedsInput): Promise<QueryAttempt<DiscoveredFeeds>> {
    const zero = (): DiscoveredFeeds => ({ finalUrl: '', feeds: [], isTruncated: false, fetchedAt: zeroTime });
    let request: DocumentRequest;
    try {
      request = { target: parseRequestURL(input.url), accept: pageAcceptHeader, validators: { etag: '', lastModified: '' }, maxBytes: this.client.pageBytes(), isPrefixRead: true, mayBeGzip: false, acceptsMediaType: isHTMLMediaType, expectedContent: 'an HTML page' };
    } catch (error) { return attemptFromFailure('discoverFeeds', defectFailure(error), zero()); }
    const fetched = await fetchDocument(this.client.settings(call), request);
    if ('failure' in fetched) return attemptFromFailure('discoverFeeds', fetched.failure, zero());
    const document = fetched.document;
    const head = scanHTMLHead(document.body, htmlContentType(document.charsetLabel));
    const base = headBaseURL(head, document.finalURL);
    const feeds: DiscoveredFeed[] = [];
    const result: DiscoveredFeeds = { finalUrl: document.finalURL.toString(), feeds, isTruncated: document.isTruncated, fetchedAt: this.client.fetchedAt() };
    if (head.title) result.pageTitle = head.title;
    const seen = new Set<string>();
    for (const link of head.links) {
      if (!hasRelation(link.relations, 'alternate') || hasRelation(link.relations, 'stylesheet') || !isAdvertisedFeedMediaType(link.mediaType)) continue;
      // A fragment never changes the fetched document, so it neither distinguishes nor follows a feed URL.
      const feedURL = resolveLink(base, link.href).split('#', 1)[0] ?? '';
      if (feedURL === '' || seen.has(feedURL) || feeds.length === maxDiscoveredFeeds) continue;
      seen.add(feedURL);
      const feed: DiscoveredFeed = { url: feedURL, mediaType: link.mediaType };
      const title = boundedText(link.title, maxTitleCharacters);
      if (title) feed.title = title;
      feeds.push(feed);
    }
    return newQueryBranch('discovered', result);
  }
}

/** Reads one sitemap or sitemap index per invocation, following redirects. */
export class ReadSitemapOperation implements Query<ReadSitemapInput, Sitemap> {
  /** Bind the client. */ constructor(private readonly client: WebFeedClient) {}
  /** The generated readSitemap contract. */ definition(): QueryDefinition { return ReadSitemapDefinition; }
  /** Read one sitemap, decompressing gzip; a 304 to a conditional GET selects notModified. */
  async invoke(call: Call, input: ReadSitemapInput): Promise<QueryAttempt<Sitemap>> {
    const zero = (): Sitemap => ({ finalUrl: '', entries: [], sitemaps: [], totalEntryCount: 0, matchedEntryCount: 0, hasMore: false, fetchedAt: zeroTime });
    let request: DocumentRequest;
    let filter;
    try {
      const target = parseRequestURL(input.url);
      const validators = validatorsFromInput(input.etag, input.lastModified);
      filter = newSitemapFilter(input.pathPrefix, input.lastModifiedAfter, input.limit);
      request = { target, accept: sitemapAcceptHeader, validators, maxBytes: this.client.documentBytes(), isPrefixRead: false, mayBeGzip: true, acceptsMediaType: isSitemapMediaType, expectedContent: 'a sitemap' };
    } catch (error) { return attemptFromFailure('readSitemap', defectFailure(error), zero()); }
    const fetched = await fetchDocument(this.client.settings(call), request);
    if ('failure' in fetched) return attemptFromFailure('readSitemap', fetched.failure, zero());
    const document = fetched.document;
    const sitemap = withValidators<Sitemap>({ ...zero(), finalUrl: document.finalURL.toString(), fetchedAt: this.client.fetchedAt() }, document.validators);
    if (document.isNotModified) return newQueryBranch('notModified', sitemap);
    const failure = parseSitemapDocument(sitemap, document, filter);
    if (failure) return attemptFromFailure('readSitemap', failure, zero());
    return newQueryBranch('read', sitemap);
  }
}

/** Reads the head metadata of one HTML page per invocation. */
export class ReadPageMetadataOperation implements Query<ReadPageMetadataInput, PageMetadata> {
  /** Bind the client. */ constructor(private readonly client: WebFeedClient) {}
  /** The generated readPageMetadata contract. */ definition(): QueryDefinition { return ReadPageMetadataDefinition; }
  /** Read the page prefix and return its head metadata; it never runs scripts or requests linked resources. */
  async invoke(call: Call, input: ReadPageMetadataInput): Promise<QueryAttempt<PageMetadata>> {
    const zero = (): PageMetadata => ({ finalUrl: '', openGraph: {}, structuredData: {}, isTruncated: false, fetchedAt: zeroTime });
    let request: DocumentRequest;
    try {
      request = { target: parseRequestURL(input.url), accept: pageAcceptHeader, validators: { etag: '', lastModified: '' }, maxBytes: this.client.pageBytes(), isPrefixRead: true, mayBeGzip: false, acceptsMediaType: isHTMLMediaType, expectedContent: 'an HTML page' };
    } catch (error) { return attemptFromFailure('readPageMetadata', defectFailure(error), zero()); }
    const fetched = await fetchDocument(this.client.settings(call), request);
    if ('failure' in fetched) return attemptFromFailure('readPageMetadata', fetched.failure, zero());
    const document = fetched.document;
    const head = scanHTMLHead(document.body, htmlContentType(document.charsetLabel));
    const base = headBaseURL(head, document.finalURL);
    const meta = (name: string): string => head.metaByName.get(name) ?? '';
    const metadata = omitEmpty({
      finalUrl: document.finalURL.toString(), title: head.title, description: boundedText(meta('description'), maxDescriptionCharacters),
      openGraph: omitEmpty({
        title: boundedText(meta('og:title'), maxTitleCharacters), description: boundedText(meta('og:description'), maxDescriptionCharacters),
        imageUrl: resolveLink(base, firstNonEmpty(meta('og:image'), meta('og:image:secure_url'), meta('og:image:url'))),
        type: boundedText(meta('og:type'), maxNameCharacters), siteName: boundedText(meta('og:site_name'), maxNameCharacters),
      }),
      articlePublishedAt: optionalTimestamp(meta('article:published_time')), articleModifiedAt: optionalTimestamp(meta('article:modified_time')),
      structuredData: readStructuredData(head.structuredScripts), isTruncated: document.isTruncated, fetchedAt: this.client.fetchedAt(),
    }) as PageMetadata;
    for (const link of head.links) {
      if (!hasRelation(link.relations, 'canonical')) continue;
      const canonical = resolveLink(base, link.href);
      if (canonical) { metadata.canonicalUrl = canonical; break; }
    }
    const publishedAt = metadata.articlePublishedAt ?? metadata.structuredData.datePublished;
    const modifiedAt = metadata.articleModifiedAt ?? metadata.structuredData.dateModified ?? optionalTimestamp(meta('og:updated_time'));
    if (publishedAt) metadata.publishedAt = publishedAt;
    if (modifiedAt) metadata.modifiedAt = modifiedAt;
    return newQueryBranch('read', metadata);
  }
}

/** Return the first JSON-LD article node, else the first node with a headline or date; invalid scripts are skipped. */
function readStructuredData(scripts: string[]): StructuredDataMetadata {
  const state = { visited: 0, fallback: undefined as StructuredDataMetadata | undefined };
  for (const script of scripts) {
    let document: unknown;
    try { document = JSON.parse(script.trim()); } catch { continue; }
    const found = searchStructuredData(document, 0, state);
    if (found) return found;
  }
  return state.fallback ?? {};
}

function searchStructuredData(value: unknown, depth: number, state: { visited: number; fallback: StructuredDataMetadata | undefined }): StructuredDataMetadata | undefined {
  if (depth > maxStructuredDataDepth || state.visited >= maxStructuredDataNodes) return undefined;
  state.visited++;
  if (Array.isArray(value)) {
    for (const element of value) {
      const found = searchStructuredData(element, depth + 1, state);
      if (found) return found;
    }
  } else if (typeof value === 'object' && value !== null) {
    const node = value as Record<string, unknown>;
    const candidate = omitEmpty({
      type: firstJSONString(node['@type']),
      headline: boundedText(firstNonEmpty(firstJSONString(node.headline), firstJSONString(node.name)), maxTitleCharacters),
      datePublished: optionalTimestamp(firstJSONString(node.datePublished)),
      dateModified: optionalTimestamp(firstJSONString(node.dateModified)),
    }) as StructuredDataMetadata;
    if (isArticleType(node['@type'])) return candidate;
    if (!state.fallback && (candidate.datePublished !== undefined || firstJSONString(node.headline) !== '')) state.fallback = candidate;
    for (const key of ['@graph', 'mainEntity', 'mainEntityOfPage']) {
      if (!(key in node)) continue;
      const found = searchStructuredData(node[key], depth + 1, state);
      if (found) return found;
    }
  }
  return undefined;
}

function isArticleType(value: unknown): boolean {
  if (typeof value === 'string') return articleTypes.has(value.replace(/^https:\/\/schema\.org\//, '').replace(/^http:\/\/schema\.org\//, ''));
  return Array.isArray(value) && value.some(isArticleType);
}

/** Read a JSON-LD value that is a string, a list starting with a string, or a value object. */
function firstJSONString(value: unknown): string {
  if (typeof value === 'string') return value.trim();
  if (Array.isArray(value)) {
    for (const element of value) {
      const text = firstJSONString(element);
      if (text !== '') return text;
    }
    return '';
  }
  if (typeof value === 'object' && value !== null) return firstJSONString((value as Record<string, unknown>)['@value']);
  return '';
}

function attemptFromFailure<Output>(operationID: string, failure: FetchFailure, zero: Output): QueryAttempt<Output> {
  const failureValue: Failure = { kind: failure.kind, provider: ProviderID, operation: operationID, message: failure.message };
  if (failure.class === 'retry') return newQueryRetry(failureValue, failure.retryAfterMs);
  return newQueryBranch(failure.class, zero, {}, failureValue);
}

function withValidators<Value extends { etag?: string; lastModified?: string }>(value: Value, validators: { etag: string; lastModified: string }): Value {
  const result = { ...value };
  delete result.etag; delete result.lastModified;
  if (validators.etag) result.etag = validators.etag;
  if (validators.lastModified) result.lastModified = validators.lastModified;
  return result;
}

// Drop empty strings and undefined values, as Go omitempty does for the optional fields.
function omitEmpty<Value extends Record<string, unknown>>(value: Value): Value {
  return Object.fromEntries(Object.entries(value).filter(([, field]) => field !== '' && field !== undefined)) as Value;
}

function htmlContentType(charsetLabel: string): string { return charsetLabel ? `text/html; charset=${charsetLabel}` : 'text/html'; }

function optionalTimestampFromDate(date: Date): string { return date.toISOString().replace(/\.000Z$/, 'Z').replace(/(\.\d*?)0+Z$/, '$1Z'); }

/** Accept feed, XML, JSON, and text types; a mislabeled text/html feed still parses. */
export function isFeedMediaType(mediaType: string): boolean {
  if (['', 'application/octet-stream', 'application/gzip', 'application/x-gzip', 'application/xml', 'application/json'].includes(mediaType)) return true;
  if (mediaType.startsWith('text/')) return mediaType !== 'text/css' && mediaType !== 'text/javascript';
  return mediaType.startsWith('application/') && (mediaType.endsWith('+xml') || mediaType.endsWith('+json'));
}

/** Accept the feed media types sites advertise; application/json is excluded because sites also advertise API documents with it. */
function isAdvertisedFeedMediaType(mediaType: string): boolean {
  return mediaType === 'application/rss+xml' || mediaType === 'application/atom+xml' || mediaType === 'application/feed+json';
}
