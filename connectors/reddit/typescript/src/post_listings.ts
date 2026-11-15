// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { encodeQueryValues, readErrorTokens } from '@superdurable/connectors-sdk/providerhttp';
import { ListSubredditPostsDefinition, SearchPostsDefinition } from './generated/definitions.js';
import type { ListSubredditPostsInput, ListedPost, PostPage, SearchPostsInput } from './generated/models.js';
import { isGoSpace, type RedditClient, type RedditExchange } from './client.js';

/** Number of posts a page holds when `limit` is blank or zero, as Reddit defaults. */
export const DefaultPostPageLimit = 25;
/** Most posts Reddit returns on one listing or search page. */
export const MaxPostPageLimit = 100;
/** Most subreddit names one `listSubredditPosts` input may join with `+`. */
export const MaxSubredditsPerListing = 10;
/** Reddit's documented search query limit, counted in Unicode code points. */
export const MaxSearchQueryCharacters = 512;
/** Bound on each `ListedPost.selfText` in UTF-8 bytes; longer text is cut at a character boundary and flagged. */
export const MaxSelfTextBytes = 4 << 10;

// Post pages live on the web origin; the configured API origin serves only OAuth API requests.
const redditWebOrigin = 'https://www.reddit.com';
// 9999-12-31T23:59:59Z, the last instant RFC 3339 can carry.
const latestRepresentableUnixSecond = 253402300799;
const subredditNamePattern = /^[A-Za-z0-9][A-Za-z0-9_]{1,20}$/;
const postIdPattern = /^t3_[a-z0-9]{1,20}$/;
// Reddit's 403 reason tokens for a community the account may not read.
const subredditAccessReasons = new Set(['private', 'quarantined', 'gated', 'gold_only', 'banned']);
const subredditPostSorts = new Set(['new', 'hot', 'top', 'rising']);
const searchPostSorts = new Set(['new', 'relevance', 'top', 'hot', 'comments']);
const postTimeWindows = new Set(['hour', 'day', 'week', 'month', 'year', 'all']);

/** Read one page of an approved Data API subreddit listing. */
export class ListSubredditPostsOperation implements Query<ListSubredditPostsInput, PostPage> {
  /** Bind the provider client. */ constructor(private readonly client: RedditClient) {}
  /** Return the generated subreddit listing Query contract. */ definition(): QueryDefinition { return ListSubredditPostsDefinition; }
  /** Read GET /r/{subreddit}/{sort} and classify the provider outcome. */
  async invoke(call: Call, input: ListSubredditPostsInput): Promise<QueryAttempt<PostPage>> {
    const request = subredditListingRequest(input);
    if ('validationMessage' in request) return selectFailure(listSubredditPostsBranches, 'defect', 'VALIDATION', request.validationMessage);
    return readPostPage(this.client, call, listSubredditPostsBranches, request.pathAndQuery, true);
  }
}

/** Read one page of approved Data API post search results. */
export class SearchPostsOperation implements Query<SearchPostsInput, PostPage> {
  /** Bind the provider client. */ constructor(private readonly client: RedditClient) {}
  /** Return the generated post search Query contract. */ definition(): QueryDefinition { return SearchPostsDefinition; }
  /** Read GET /search, or /r/{subreddit}/search restricted to that subreddit, and classify the outcome. */
  async invoke(call: Call, input: SearchPostsInput): Promise<QueryAttempt<PostPage>> {
    const request = postSearchRequest(input);
    if ('validationMessage' in request) return selectFailure(searchPostsBranches, 'defect', 'VALIDATION', request.validationMessage);
    return readPostPage(this.client, call, searchPostsBranches, request.pathAndQuery, !!input.subreddit);
  }
}

/** A provider path with its query, or the reason the input selects defect before any request. */
type PostPageRequest = { pathAndQuery: string } | { validationMessage: string };

interface PostPageBranches {
  operation: string;
  succeeded: string;
}

const listSubredditPostsBranches: PostPageBranches = { operation: 'listSubredditPosts', succeeded: 'listed' };
const searchPostsBranches: PostPageBranches = { operation: 'searchPosts', succeeded: 'searched' };

/** Send one listing or search request and select a branch or Retry from the response. */
async function readPostPage(client: RedditClient, call: Call, branches: PostPageBranches, pathAndQuery: string, isSubredditScoped: boolean): Promise<QueryAttempt<PostPage>> {
  const exchange = await client.get(call, pathAndQuery);
  if (exchange.error === 'credential') return selectFailure(branches, 'defect', 'AUTHENTICATION', 'Reddit OAuth credentials are unavailable or invalid');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry(branches, 'TRANSPORT', 'Reddit request failed');
  if (exchange.status === 429) return retry(branches, 'RATE_LIMIT', 'Reddit rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry(branches, 'AVAILABILITY', 'Reddit is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return selectFailure(branches, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'Reddit response exceeded the configured limit');
  if (exchange.error) return retry(branches, 'TRANSPORT', 'Reddit response was interrupted');
  if (isSubredditScoped) {
    const kind = subredditUnavailableFailureKind(exchange);
    if (kind) return selectFailure(branches, 'subredditUnavailable', kind, 'Reddit reports the subreddit missing, banned, private, or restricted');
  }
  if (exchange.status !== 200) return selectFailure(branches, 'providerRejected', 'PROVIDER_REJECTION', `Reddit rejected HTTP ${exchange.status}`);
  const page = decodePostPage(exchange.body);
  if (!page) return selectFailure(branches, 'invalidResponse', 'PROTOCOL', 'Reddit returned an invalid post listing');
  return newQueryBranch(branches.succeeded, page);
}

function selectFailure(branches: PostPageBranches, branchId: string, kind: Failure['kind'], message: string): QueryAttempt<PostPage> {
  return newQueryBranch(branchId, emptyPage(), {}, { kind, provider: 'reddit', operation: branches.operation, message });
}
function retry(branches: PostPageBranches, kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<PostPage> {
  return newQueryRetry({ kind, provider: 'reddit', operation: branches.operation, message }, delayMs);
}
function emptyPage(): PostPage { return { posts: [] }; }

/** Recognize Reddit's unknown-name redirect, banned 404, and access-reason 403. */
function subredditUnavailableFailureKind(exchange: RedditExchange): Failure['kind'] | undefined {
  if (exchange.status >= 300 && exchange.status < 400 && isSubredditSearchRedirect(exchange.location)) return 'NOT_FOUND';
  if (exchange.status === 404) return 'NOT_FOUND';
  if (exchange.status === 403 && readErrorTokens(exchange.body, ['/reason']).some(reason => subredditAccessReasons.has(reason))) return 'AUTHORIZATION';
  return undefined;
}

// Mirrors Go url.Parse: the unescaped path of an absolute or relative reference.
function isSubredditSearchRedirect(location: string): boolean {
  let reference = location;
  const scheme = /^[A-Za-z][A-Za-z0-9+.-]*:/.exec(reference);
  if (scheme) reference = reference.slice(scheme[0].length);
  if (reference.startsWith('//')) {
    const pathStart = reference.slice(2).search(/[/?#]/);
    reference = pathStart < 0 ? '' : reference.slice(2 + pathStart);
  }
  const pathEnd = reference.search(/[?#]/);
  try { return decodeURIComponent(pathEnd < 0 ? reference : reference.slice(0, pathEnd)).startsWith('/subreddits/search'); }
  catch { return false; }
}

function subredditListingRequest(input: ListSubredditPostsInput): PostPageRequest {
  if (!input || !isValidSubredditPath(input.subreddit, MaxSubredditsPerListing))
    return { validationMessage: 'Reddit subreddit must be 1 through 10 community names joined with +, without r/' };
  const sort = input.sort || 'new';
  if (!subredditPostSorts.has(sort)) return { validationMessage: 'Reddit listing sort must be new, hot, top, or rising' };
  const query = postPageQuery(input.limit, input.after);
  if (typeof query === 'string') return { validationMessage: query };
  if (sort === 'top') {
    const timeWindow = input.timeWindow || 'day';
    if (!postTimeWindows.has(timeWindow)) return { validationMessage: 'Reddit time window must be hour, day, week, month, year, or all' };
    query.t = [timeWindow];
  } else if (input.timeWindow) {
    return { validationMessage: 'Reddit listing time window applies only to the top sort' };
  }
  return { pathAndQuery: `/r/${input.subreddit}/${sort}?${encodeQueryValues(query)}` };
}

function postSearchRequest(input: SearchPostsInput): PostPageRequest {
  if (!input || typeof input.query !== 'string' || isGoBlank(input.query) || Array.from(input.query).length > MaxSearchQueryCharacters)
    return { validationMessage: 'Reddit search query must be non-blank UTF-8 text of at most 512 characters' };
  if (input.subreddit && !isValidSubredditPath(input.subreddit, 1)) return { validationMessage: 'Reddit search subreddit must be one community name without r/' };
  const sort = input.sort || 'new';
  if (!searchPostSorts.has(sort)) return { validationMessage: 'Reddit search sort must be new, relevance, top, hot, or comments' };
  const timeWindow = input.timeWindow || 'all';
  if (!postTimeWindows.has(timeWindow)) return { validationMessage: 'Reddit time window must be hour, day, week, month, year, or all' };
  const query = postPageQuery(input.limit, input.after);
  if (typeof query === 'string') return { validationMessage: query };
  Object.assign(query, { q: [input.query], type: ['link'], sort: [sort], t: [timeWindow] });
  if (!input.subreddit) return { pathAndQuery: `/search?${encodeQueryValues(query)}` };
  query.restrict_sr = ['true'];
  return { pathAndQuery: `/r/${input.subreddit}/search?${encodeQueryValues(query)}` };
}

/** Validate the shared page bounds and cursor and return their query parameters, or the validation message. */
function postPageQuery(limit: number | undefined, after: string | undefined): Record<string, string[]> | string {
  const pageLimit = limit ?? 0;
  if (!Number.isSafeInteger(pageLimit) || pageLimit < 0 || pageLimit > MaxPostPageLimit) return 'Reddit page limit must be 1 through 100; blank or 0 reads 25';
  if (after && !postIdPattern.test(after)) return 'Reddit after cursor must be a full t3_ post ID from the previous page';
  const query: Record<string, string[]> = { limit: [String(pageLimit || DefaultPostPageLimit)], raw_json: ['1'] };
  if (after) query.after = [after];
  return query;
}

function isValidSubredditPath(value: unknown, maximumNames: number): boolean {
  if (typeof value !== 'string') return false;
  const names = value.split('+');
  return names.length <= maximumNames && names.every(name => subredditNamePattern.test(name));
}

// Go strings.TrimSpace treats exactly the unicode.IsSpace code points as space; JavaScript trim differs.
function isGoBlank(value: string): boolean {
  return Array.from(value).every(character => isGoSpace(character.codePointAt(0) ?? 0));
}

function decodePostPage(body: Uint8Array): PostPage | undefined {
  let document: unknown;
  try { document = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(document) || !isObjectOrNull(document.data)) return undefined;
  const data = document.data ?? {};
  const children = data.children;
  if (document.kind !== 'Listing' || !Array.isArray(children) || children.length > MaxPostPageLimit) return undefined;
  const after = data.after ?? '';
  if (typeof after !== 'string' || (after !== '' && !postIdPattern.test(after))) return undefined;
  const posts: ListedPost[] = [];
  for (const child of children) {
    if (!isObject(child) || child.kind !== 't3' || !isObjectOrNull(child.data)) return undefined;
    const post = convertListedPost(child.data ?? {});
    if (!post) return undefined;
    posts.push(post);
  }
  return after ? { posts, after } : { posts };
}

interface RedditListedPostData {
  name: string; title: string; subreddit: string; author: string; permalink: string; url: string; selfText: string;
  isSelf: boolean; isOver18: boolean; isSpoiler: boolean; isStickied: boolean; score: number; commentCount: number; createdUTC: number;
}

// Go decodes a missing or null JSON value as the field's zero value and rejects a value of another type.
function decodeListedPostData(data: Record<string, unknown>): RedditListedPostData | undefined {
  const fields = {
    name: text(data.name), title: text(data.title), subreddit: text(data.subreddit), author: text(data.author),
    permalink: text(data.permalink), url: text(data.url), selfText: text(data.selftext),
    isSelf: flag(data.is_self), isOver18: flag(data.over_18), isSpoiler: flag(data.spoiler), isStickied: flag(data.stickied),
    score: integer(data.score), commentCount: integer(data.num_comments), createdUTC: float(data.created_utc),
  };
  return Object.values(fields).includes(undefined) ? undefined : fields as RedditListedPostData;
}

function convertListedPost(raw: Record<string, unknown>): ListedPost | undefined {
  const data = decodeListedPostData(raw);
  if (!data || !postIdPattern.test(data.name) || !data.title || !data.subreddit ||
    !data.permalink.startsWith('/r/') || /[\0\r\n]/.test(data.permalink)) return undefined;
  if (data.createdUTC <= 0 || data.createdUTC > latestRepresentableUnixSecond) return undefined;
  const [selfText, isSelfTextTruncated] = truncateUTF8(data.selfText, MaxSelfTextBytes);
  const linkUrl = linkedURL(data.isSelf, data.url);
  return {
    id: data.name, title: data.title, subreddit: data.subreddit, author: data.author, permalink: redditWebOrigin + data.permalink,
    ...(linkUrl ? { linkUrl } : {}), isSelf: data.isSelf,
    ...(selfText ? { selfText } : {}), ...(isSelfTextTruncated ? { isSelfTextTruncated } : {}),
    score: data.score, commentCount: data.commentCount,
    createdAt: new Date(Math.trunc(data.createdUTC) * 1000).toISOString().replace('.000Z', 'Z'),
    isOver18: data.isOver18, isSpoiler: data.isSpoiler, isStickied: data.isStickied,
  };
}

/** Return a link post's absolute HTTP(S) target; a crosspost's relative Reddit path joins the web origin. */
function linkedURL(isSelf: boolean, url: string): string {
  // Go url.Parse rejects ASCII control characters; spaces are rejected too.
  if (isSelf || !url || /[\0-\x20\x7f]/.test(url)) return '';
  const target = url.startsWith('/') && !url.startsWith('//') ? redditWebOrigin + url : url;
  const parts = /^https?:\/\/([^/?#]*)([^?#]*)(?:\?[^#]*)?(#.*)?$/i.exec(target);
  if (!parts) return '';
  const [, authority = '', path = '', fragment = ''] = parts;
  const userinfoEnd = authority.lastIndexOf('@');
  const userinfo = userinfoEnd < 0 ? '' : authority.slice(0, userinfoEnd);
  const host = authority.slice(userinfoEnd + 1);
  const portStart = host.startsWith('[') ? host.indexOf(']') + 1 : host.lastIndexOf(':');
  const port = portStart > 0 && portStart < host.length ? host.slice(portStart) : '';
  if (!host || (host.startsWith('[') && portStart === 0) || (port && !/^:[0-9]*$/.test(port))) return '';
  // Go url.Parse limits userinfo to unreserved and sub-delimiter characters, and a host below U+0080 also to brackets.
  if (!/^[A-Za-z0-9\-._:~!$&'()*+,;=%@]*$/.test(userinfo)) return '';
  if (/[^A-Za-z0-9\-._~!$&'()*+,;=:[\]<>"%\u0080-\uffff]/.test(host)) return '';
  if ([userinfo, host, path, fragment].some(component => /%(?![0-9A-Fa-f]{2})/.test(component))) return '';
  // A host escape must encode a byte of a multibyte character, or be %25.
  if (/%(?!25)[0-7][0-9A-Fa-f]/.test(host)) return '';
  return target;
}

function truncateUTF8(value: string, maxBytes: number): [string, boolean] {
  const bytes = Buffer.from(value, 'utf8');
  if (bytes.length <= maxBytes) return [value, false];
  let cut = maxBytes;
  while (cut > 0 && ((bytes[cut] ?? 0) & 0xc0) === 0x80) cut--;
  return [bytes.subarray(0, cut).toString('utf8'), true];
}

function text(value: unknown): string | undefined { return value === undefined || value === null ? '' : typeof value === 'string' ? value : undefined; }
function flag(value: unknown): boolean | undefined { return value === undefined || value === null ? false : typeof value === 'boolean' ? value : undefined; }
function integer(value: unknown): number | undefined { return value === undefined || value === null ? 0 : Number.isSafeInteger(value) ? value as number : undefined; }
function float(value: unknown): number | undefined { return value === undefined || value === null ? 0 : typeof value === 'number' ? value : undefined; }
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function isObjectOrNull(value: unknown): value is Record<string, unknown> | null | undefined { return value === undefined || value === null || isObject(value); }
