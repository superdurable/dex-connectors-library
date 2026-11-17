// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newDiscoverFeedsStep, newReadFeedStep, newReadPageMetadataStep, newReadPageStep, newReadSitemapStep, type Connection,
  type DiscoveredFeeds, type DiscoverFeedsInput, type Feed, type FeedItem, type Page, type PageMetadata, type ReadFeedInput,
  type ReadPageInput, type ReadPageMetadataInput, type ReadSitemapInput, type Sitemap, type SitemapEntry,
} from '@superdurable/connector-web-feed';

/** Static Dex Web connection of the web-feed connector. */
export const ConnectionName = 'web-feed-public';
/** Report source for posts read from a feed. */
export const SourceFeed = 'feed';
/** Report source for posts read from a sitemap and the post pages. */
export const SourceSitemap = 'sitemap';
const defaultMaxPosts = 5;
const maxPostsAllowed = 10;
// Bounds the sitemap and child sitemap reads of one check.
const maxSitemapReads = 4;
// Bounds the reported post text, which the report keeps as an excerpt.
const firstPostTextCharacters = 2000;

/** Blog to check; give siteUrl, feedUrl, or sitemapUrl. */
export interface CheckRequest {
  /** Blog home or index page whose advertised feeds are discovered first. */ siteUrl?: string;
  /** Feed to read without discovery. */ feedUrl?: string;
  /** Sitemap read when no feed is found; empty uses /sitemap.xml at the page origin. */ sitemapUrl?: string;
  /** Keeps only sitemap entries under this path, such as /blog/. */ pathPrefix?: string;
  /** RFC 3339 time; only newer posts are reported. */ publishedAfter?: string;
  /** Reported posts, from 1 through 10; zero selects 5. */ maxPosts?: number;
  /** Feed etag a previous check reported, sent as a conditional GET validator. */ etag?: string;
  /** Feed lastModified a previous check reported, sent as a conditional GET validator. */ lastModified?: string;
  /** Reads the readable text of the first reported post into the report. */ includeFirstPostText?: boolean;
}

/** One reported post; title and summary are untrusted provider text. */
export interface Post { url: string; title?: string; summary?: string; publishedAt?: string }

/** Flow output: the newest posts and the feed validators for the next check. */
export interface CheckReport {
  source: string; sourceUrl: string; isUnchanged: boolean; etag?: string; lastModified?: string; posts: Post[];
  /** Readable text of the first post, at most 2000 code points, when the request set includeFirstPostText; untrusted provider text. */
  firstPostText?: string;
}

interface CheckState {
  request: CheckRequest & { maxPosts: number };
  publishedAfter?: string;
  sitemapReadCount: number;
  pendingSitemaps: string[];
  candidates: SitemapEntry[];
  pendingPages: SitemapEntry[];
  report?: CheckReport;
}

const checkStateAttribute = new Attribute<CheckState>('blog-post-check-state', jsonCodec<CheckState>());
const reportAttribute = new Attribute<CheckReport>('blog-post-check-report', jsonCodec<CheckReport>());

/** Check one blog: its advertised feed, or for a blog without one, its sitemap and newest post pages. */
export class BlogPostCheck implements Flow<CheckRequest> {
  private readonly start = new StartBlogPostCheck();
  private readonly chooseSource = new ChooseBlogSource();
  private readonly collectFeedPosts = new CollectFeedPosts();
  private readonly completeUnchangedFeed = new CompleteUnchangedFeed();
  private readonly collectSitemapEntries = new CollectSitemapEntries();
  private readonly collectPostPage = new CollectPostPage();
  private readonly collectFirstPostText = new CollectFirstPostText();

  /** Bind the static web-feed connection. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('the blog post check requires its static connection');
  }

  /** Stable Flow identity, matching the Go example. */ getFlowType(): string { return 'BlogPostCheck'; }

  /** Register the application Steps and one Connector Step per web-feed operation. */
  getSteps(): StepList<CheckRequest> {
    return StepList.startStep<CheckRequest>(this.start).otherSteps(
      newDiscoverFeedsStep<DiscoverFeedsInput>({
        stepType: 'DiscoverBlogFeeds', connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input,
        annotations: { groupId: 'source', groupLabel: 'Source', explanation: 'List the feeds the blog page advertises.' },
        discovered: this.chooseSource,
      }),
      this.chooseSource,
      newReadFeedStep<ReadFeedInput>({
        stepType: 'ReadBlogFeed', connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input,
        annotations: { groupId: 'feed', groupLabel: 'Feed', explanation: 'Read the blog feed with a conditional GET.' },
        read: this.collectFeedPosts,
        // An unchanged feed completes the check without posts and keeps the validators for the next poll.
        notModified: this.completeUnchangedFeed,
      }),
      this.collectFeedPosts, this.completeUnchangedFeed,
      newReadSitemapStep<ReadSitemapInput>({
        stepType: 'ReadBlogSitemap', connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input,
        annotations: { groupId: 'sitemap', groupLabel: 'Sitemap', explanation: 'Read one sitemap filtered by path and date.' },
        read: this.collectSitemapEntries,
      }),
      this.collectSitemapEntries,
      newReadPageMetadataStep<ReadPageMetadataInput>({
        stepType: 'ReadPostPage', connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input,
        annotations: { groupId: 'sitemap', groupLabel: 'Sitemap', explanation: 'Read the title and dates of one recent post page.' },
        read: this.collectPostPage,
        // A post removed after the sitemap listed it is skipped so the check reports the others.
        notFound: this.collectPostPage,
      }),
      this.collectPostPage,
      newReadPageStep<ReadPageInput>({
        stepType: 'ReadFirstPostText', connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input,
        annotations: { groupId: 'text', groupLabel: 'Post text', explanation: 'Read the readable text of the first reported post.' },
        read: this.collectFirstPostText,
      }),
      this.collectFirstPostText,
    );
  }

  /** Register the check state and the report. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [checkStateAttribute, reportAttribute] }; }
}

// dex:group group-id:source group-label:"Source"
// dex:explanation text:"Validate the request and read the feed, the page, or the sitemap it names."
class StartBlogPostCheck implements Step<CheckRequest> {
  getStepType(): string { return 'StartBlogPostCheck'; }
  execute(context: Context, request: CheckRequest): StepDecision {
    let state: CheckState;
    try { state = newCheckState(request); } catch (error) { return forceFail(error instanceof Error ? error.message : 'the request is invalid'); }
    const isSitemapOnly = !request.feedUrl && !request.siteUrl;
    if (isSitemapOnly) state.sitemapReadCount = 1;
    checkStateAttribute.set(context, state);
    if (request.feedUrl) return goTo(new StepRef('ReadBlogFeed'), feedInput(state, request.feedUrl));
    if (request.siteUrl) return goTo(new StepRef('DiscoverBlogFeeds'), { url: request.siteUrl } satisfies DiscoverFeedsInput);
    return goTo(new StepRef('ReadBlogSitemap'), sitemapInput(state, request.sitemapUrl ?? ''));
  }
}

// dex:group group-id:source group-label:"Source"
// dex:explanation text:"Choose the first advertised feed, or the sitemap when the page has no feed."
class ChooseBlogSource implements Step<QueryResult<DiscoveredFeeds>> {
  readonly inputCodec = jsonCodec<QueryResult<DiscoveredFeeds>>();
  getStepType(): string { return 'ChooseBlogSource'; }
  execute(context: Context, result: QueryResult<DiscoveredFeeds>): StepDecision {
    const state = checkStateAttribute.get(context);
    const firstFeed = result.value.feeds?.[0];
    if (firstFeed) return goTo(new StepRef('ReadBlogFeed'), feedInput(state, firstFeed.url));
    state.sitemapReadCount = 1;
    checkStateAttribute.set(context, state);
    return goTo(new StepRef('ReadBlogSitemap'), sitemapInput(state, state.request.sitemapUrl || defaultSitemapURL(result.value.finalUrl)));
  }
}

// dex:group group-id:feed group-label:"Feed"
// dex:explanation text:"Report the newest feed items and the validators for the next check."
class CollectFeedPosts implements Step<QueryResult<Feed>> {
  readonly inputCodec = jsonCodec<QueryResult<Feed>>();
  getStepType(): string { return 'CollectFeedPosts'; }
  execute(context: Context, result: QueryResult<Feed>): StepDecision {
    const state = checkStateAttribute.get(context);
    state.report = {
      source: SourceFeed, sourceUrl: result.value.finalUrl, isUnchanged: false,
      ...validators(result.value), posts: selectFeedPosts(result.value.items ?? [], state.publishedAfter, state.request.maxPosts),
    };
    if (shouldReadFirstPostText(state)) {
      checkStateAttribute.set(context, state);
      return goTo(new StepRef('ReadFirstPostText'), firstPostTextInput(state));
    }
    reportAttribute.set(context, state.report);
    return gracefulComplete(state.report);
  }
}

// dex:group group-id:feed group-label:"Feed"
// dex:explanation text:"Report that the feed is unchanged and keep its validators."
class CompleteUnchangedFeed implements Step<QueryResult<Feed>> {
  readonly inputCodec = jsonCodec<QueryResult<Feed>>();
  getStepType(): string { return 'CompleteUnchangedFeed'; }
  execute(context: Context, result: QueryResult<Feed>): StepDecision {
    const report: CheckReport = { source: SourceFeed, sourceUrl: result.value.finalUrl, isUnchanged: true, ...validators(result.value), posts: [] };
    reportAttribute.set(context, report);
    return gracefulComplete(report);
  }
}

// dex:group group-id:sitemap group-label:"Sitemap"
// dex:explanation text:"Queue child sitemaps, then the newest matching post pages."
class CollectSitemapEntries implements Step<QueryResult<Sitemap>> {
  readonly inputCodec = jsonCodec<QueryResult<Sitemap>>();
  getStepType(): string { return 'CollectSitemapEntries'; }
  execute(context: Context, result: QueryResult<Sitemap>): StepDecision {
    const state = checkStateAttribute.get(context);
    state.report ??= { source: SourceSitemap, sourceUrl: result.value.finalUrl, isUnchanged: false, posts: [] };
    recordSitemap(state, result.value);
    const next = state.pendingSitemaps.shift();
    if (next !== undefined) {
      state.sitemapReadCount++;
      checkStateAttribute.set(context, state);
      return goTo(new StepRef('ReadBlogSitemap'), sitemapInput(state, next));
    }
    state.pendingPages = newestEntries(state.candidates, state.request.maxPosts);
    checkStateAttribute.set(context, state);
    const firstPage = state.pendingPages[0];
    if (!firstPage) {
      if (shouldReadFirstPostText(state)) {
        checkStateAttribute.set(context, state);
        return goTo(new StepRef('ReadFirstPostText'), firstPostTextInput(state));
      }
      reportAttribute.set(context, state.report);
      return gracefulComplete(state.report);
    }
    return goTo(new StepRef('ReadPostPage'), { url: firstPage.url } satisfies ReadPageMetadataInput);
  }
}

// dex:group group-id:sitemap group-label:"Sitemap"
// dex:explanation text:"Add the post page's title and dates, then read the next page or complete."
class CollectPostPage implements Step<QueryResult<PageMetadata>> {
  readonly inputCodec = jsonCodec<QueryResult<PageMetadata>>();
  getStepType(): string { return 'CollectPostPage'; }
  execute(context: Context, result: QueryResult<PageMetadata>): StepDecision {
    const state = checkStateAttribute.get(context);
    const entry = state.pendingPages.shift();
    if (!entry || !state.report) throw new Error('blog post check state has no pending post page');
    if (result.branch === 'read') state.report.posts.push(postFromPage(entry, result.value));
    checkStateAttribute.set(context, state);
    const nextPage = state.pendingPages[0];
    if (nextPage) return goTo(new StepRef('ReadPostPage'), { url: nextPage.url } satisfies ReadPageMetadataInput);
    if (shouldReadFirstPostText(state)) {
      checkStateAttribute.set(context, state);
      return goTo(new StepRef('ReadFirstPostText'), firstPostTextInput(state));
    }
    reportAttribute.set(context, state.report);
    return gracefulComplete(state.report);
  }
}

// dex:group group-id:text group-label:"Post text"
// dex:explanation text:"Add the first post's readable text to the report and complete."
class CollectFirstPostText implements Step<QueryResult<Page>> {
  readonly inputCodec = jsonCodec<QueryResult<Page>>();
  getStepType(): string { return 'CollectFirstPostText'; }
  execute(context: Context, result: QueryResult<Page>): StepDecision {
    const state = checkStateAttribute.get(context);
    if (!state.report) throw new Error('blog post check state has no report');
    state.report.firstPostText = result.value.text;
    reportAttribute.set(context, state.report);
    return gracefulComplete(state.report);
  }
}

/** Validate a start request; exported for deterministic tests. */
export function newCheckState(request: CheckRequest): CheckState {
  if (!request.siteUrl && !request.feedUrl && !request.sitemapUrl) throw new TypeError('siteUrl, feedUrl, or sitemapUrl is required');
  const maxPosts = request.maxPosts || defaultMaxPosts;
  if (!Number.isSafeInteger(maxPosts) || maxPosts < 1 || maxPosts > maxPostsAllowed) throw new TypeError(`maxPosts must be from 1 through ${maxPostsAllowed}`);
  const state: CheckState = { request: { ...request, maxPosts }, sitemapReadCount: 0, pendingSitemaps: [], candidates: [], pendingPages: [] };
  if (request.publishedAfter) {
    if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(request.publishedAfter) || Number.isNaN(Date.parse(request.publishedAfter))) {
      throw new TypeError('publishedAfter must be an RFC 3339 time, such as 2026-11-01T00:00:00Z');
    }
    state.publishedAfter = new Date(request.publishedAfter).toISOString().replace('.000Z', 'Z');
  }
  return state;
}

/** Whether the request asked for the first post's text and a post has a URL. */
function shouldReadFirstPostText(state: CheckState): boolean {
  return Boolean(state.request.includeFirstPostText && state.report?.posts[0]?.url);
}

/** Read the first reported post's text within the report's bound; exported for deterministic tests. */
export function firstPostTextInput(state: CheckState): ReadPageInput {
  return { url: state.report?.posts[0]?.url ?? '', maxTextCharacters: firstPostTextCharacters };
}

function feedInput(state: CheckState, url: string): ReadFeedInput {
  return { url, etag: state.request.etag, lastModified: state.request.lastModified, limit: 50, maxSummaryCharacters: 300 };
}

function sitemapInput(state: CheckState, url: string): ReadSitemapInput {
  return { url, pathPrefix: state.request.pathPrefix, lastModifiedAfter: state.publishedAfter, limit: state.request.maxPosts };
}

function validators(feed: Feed): Pick<CheckReport, 'etag' | 'lastModified'> {
  return { ...(feed.etag ? { etag: feed.etag } : {}), ...(feed.lastModified ? { lastModified: feed.lastModified } : {}) };
}

/** Keep up to maxPosts items in feed order, dated after publishedAfter when it is set. */
export function selectFeedPosts(items: FeedItem[], publishedAfter: string | undefined, maxPosts: number): Post[] {
  const posts: Post[] = [];
  for (const item of items) {
    const published = item.publishedAt ?? item.updatedAt ?? undefined;
    if (publishedAfter && (!published || Date.parse(published) <= Date.parse(publishedAfter))) continue;
    posts.push({ url: item.link ?? '', ...(item.title ? { title: item.title } : {}), ...(item.summary ? { summary: item.summary } : {}), ...(published ? { publishedAt: published } : {}) });
    if (posts.length === maxPosts) break;
  }
  return posts;
}

/** Queue an index's newest children within the read budget, or keep a urlset's entries. */
function recordSitemap(state: CheckState, sitemap: Sitemap): void {
  if (sitemap.kind === 'sitemapIndex') {
    for (const child of sitemap.sitemaps ?? []) {
      if (state.sitemapReadCount + state.pendingSitemaps.length >= maxSitemapReads) break;
      state.pendingSitemaps.push(child.url);
    }
    return;
  }
  state.candidates.push(...(sitemap.entries ?? []));
}

/** Merge the entries of every read sitemap, newest first, without repeated URLs. */
export function newestEntries(candidates: SitemapEntry[], maxPosts: number): SitemapEntry[] {
  const time = (entry: SitemapEntry): number | undefined => {
    const value = entry.lastModifiedAt ?? entry.newsPublishedAt;
    return value ? Date.parse(value) : undefined;
  };
  const sorted = [...candidates].sort((left, right) => {
    const leftTime = time(left); const rightTime = time(right);
    if (leftTime === undefined) return rightTime === undefined ? 0 : 1;
    return rightTime === undefined ? -1 : rightTime - leftTime;
  });
  const selected: SitemapEntry[] = [];
  const seen = new Set<string>();
  for (const entry of sorted) {
    if (seen.has(entry.url)) continue;
    seen.add(entry.url);
    selected.push(entry);
    if (selected.length === maxPosts) break;
  }
  return selected;
}

function postFromPage(entry: SitemapEntry, metadata: PageMetadata): Post {
  const title = [metadata.openGraph.title, metadata.structuredData.headline, metadata.title, entry.newsTitle].find(value => value?.trim());
  const summary = [metadata.description, metadata.openGraph.description].find(value => value?.trim());
  const publishedAt = metadata.publishedAt ?? entry.lastModifiedAt ?? entry.newsPublishedAt ?? undefined;
  return { url: entry.url, ...(title ? { title: title.trim() } : {}), ...(summary ? { summary: summary.trim() } : {}), ...(publishedAt ? { publishedAt } : {}) };
}

/** /sitemap.xml at the origin of the page that was read. */
export function defaultSitemapURL(pageURL: string): string {
  try { return new URL('/sitemap.xml', pageURL).toString(); } catch { return pageURL; }
}
