// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, newMutationBranch, newMutationRetry, newMutationUncertain, zeroReceipt, type Call, type Failure, type Query, type Mutation, type QueryAttempt, type MutationAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { SearchVideosDefinition, GetVideoDefinition, GetChannelDefinition, ListPlaylistItemsDefinition, AddPlaylistItemDefinition } from './generated/definitions.js';
import type { SearchVideosInput, VideoSearchPage, VideoSearchResult, GetVideoInput, Video, GetChannelInput, Channel, ListPlaylistItemsInput, PlaylistItemPage, PlaylistItem, AddPlaylistItemInput, AddedPlaylistItem } from './generated/models.js';
import { Fence } from '@superdurable/connectors-sdk/singledispatch';
import { YouTubeExchangeError, type YouTubeClient, type YouTubeResponse } from './client.js';

const videoID = /^[A-Za-z0-9_-]{11}$/;
const channelID = /^UC[A-Za-z0-9_-]{22}$/;
const playlistID = /^[A-Za-z0-9_-]{2,64}$/;
const handle = /^@?[A-Za-z0-9._-]{3,30}$/;
const pageToken = /^[A-Za-z0-9_-]+$/;
const zeroSearch: VideoSearchPage = { results: [], totalResults: 0 };
const zeroVideo: Video = { id: '', channelId: '', channelTitle: '', title: '', description: '', publishedAt: '0001-01-01T00:00:00Z', privacyStatus: '' };
const zeroChannel: Channel = { id: '', title: '', description: '', publishedAt: '0001-01-01T00:00:00Z', uploadsPlaylistId: '' };
const zeroPage: PlaylistItemPage = { items: [], totalResults: 0 };
const zeroItem: PlaylistItem = { id: '', playlistId: '', videoId: '', title: '', position: 0, addedAt: '0001-01-01T00:00:00Z' };
const zeroAdded: AddedPlaylistItem = { item: zeroItem, wasAlreadyPresent: false };

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'youtube', operation, message }; }
function receipt(call: Call, objectID: string, response?: YouTubeResponse): ReceiptDraft {
  const headers = response?.headers;
  const requestIDValue = headers?.['x-goog-request-id'];
  const requestID = Array.isArray(requestIDValue) ? requestIDValue[0] : requestIDValue;
  return { callId: call.id, ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), provider: 'youtube', providerObjectId: objectID, observedAt: new Date().toISOString(), ...(requestID && requestID.length <= 128 && pageToken.test(requestID) ? { providerRequestId: requestID } : {}) };
}
function queryError<T>(operation: string, error: unknown, empty: T, call: Call, objectID: string, notFound = false): QueryAttempt<T> {
  if (!(error instanceof YouTubeExchangeError)) throw error;
  const detail = failure(operation, error.kind as Failure['kind'], error.message);
  if (error.disposition === 'retry') return newQueryRetry(detail, error.retryAfterMs);
  return newQueryBranch(error.disposition === 'notFound' && !notFound ? 'providerRejected' : error.disposition, empty, error.response ? receipt(call, objectID, error.response) : zeroReceipt(), detail);
}
function mutationError(operation: string, error: unknown, call: Call, objectID: string): MutationAttempt<AddedPlaylistItem> {
  if (!(error instanceof YouTubeExchangeError)) throw error;
  const detail = failure(operation, error.kind as Failure['kind'], error.message);
  if (error.disposition === 'retry') return newMutationRetry(detail, error.retryAfterMs);
  return newMutationBranch(error.disposition, zeroAdded, error.response || error.message.startsWith('addPlaylistItem requires') ? receipt(call, objectID, error.response) : zeroReceipt(), detail);
}
function defect<T>(operation: string, empty: T, message: string): QueryAttempt<T> { return newQueryBranch('defect', empty, zeroReceipt(), failure(operation, 'VALIDATION', message)); }
function invalid<T>(operation: string, empty: T, message: string, call: Call, objectID: string, response: YouTubeResponse): QueryAttempt<T> { return newQueryBranch('invalidResponse', empty, receipt(call, objectID, response), failure(operation, 'PROTOCOL', message)); }
function object(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid provider object'); return value as Record<string, unknown>; }
function field(source: Record<string, unknown>, name: string): string { const value = source[name]; if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new TypeError(`invalid provider ${name}`); return value; }
function integer(source: Record<string, unknown>, name: string): number { const value = source[name]; if (value === undefined || value === null) return 0; if (!Number.isSafeInteger(value)) throw new TypeError(`invalid provider ${name}`); return value as number; }
function optionalCount(source: Record<string, unknown>, name: string): number | undefined { const value = source[name]; if (value === undefined || value === null) return undefined; if (typeof value !== 'string' || !/^[+-]?\d+$/.test(value)) throw new TypeError('invalid statistic'); const count = Number(value); if (!Number.isSafeInteger(count) || count < 0) throw new TypeError('invalid statistic'); return count; }
function timestamp(value: string): string {
  if (!value.trim()) throw new TypeError('missing timestamp');
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!match) throw new TypeError('invalid timestamp');
  const year = Number(match[1]), month = Number(match[2]), day = Number(match[3]), hour = Number(match[4]), minute = Number(match[5]), second = Number(match[6]);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31, 30];
  if (month < 1 || month > 12 || day < 1 || day > days[month - 1]! || hour > 23 || minute > 59 || second > 59 || (match[8] !== 'Z' && Number(match[8]!.slice(-2)) > 59)) throw new TypeError('invalid timestamp');
  const fraction = (match[7] ?? '').slice(0, 9).replace(/0+$/, '');
  const calendar = new Date(`${value.slice(0, 19)}Z`);
  if (Number.isNaN(calendar.getTime())) throw new TypeError('invalid timestamp');
  const offset = match[8]!;
  const minutes = offset === 'Z' ? 0 : (Number(offset.slice(1, 3)) * 60 + Number(offset.slice(4, 6))) * (offset[0] === '+' ? 1 : -1);
  const parsed = new Date(calendar.getTime() - minutes * 60_000);
  const milliseconds = parsed.toISOString().slice(0, 19);
  return `${milliseconds}${fraction ? `.${fraction}` : ''}Z`;
}
function optionalTimestamp(value: string): string | undefined { return value.trim() ? timestamp(value) : undefined; }
function decoded(response: YouTubeResponse): Record<string, unknown> { return object(JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body))); }
function pageFields(source: Record<string, unknown>): { nextPageToken?: string; totalResults: number; items: unknown[] } {
  if (!Array.isArray(source.items) || !source.pageInfo) throw new TypeError('incomplete page');
  const next = field(source, 'nextPageToken');
  if (next && (next.length > 256 || !pageToken.test(next))) throw new TypeError('invalid nextPageToken');
  return { ...(next ? { nextPageToken: next } : {}), totalResults: integer(object(source.pageInfo), 'totalResults'), items: source.items };
}
function validatePage(query: Record<string, string>, maxResults: number | undefined, token: string | undefined): string | undefined {
  const size = maxResults ?? 0;
  if (!Number.isSafeInteger(size) || size < 0 || size > 50) return "maxResults must be from 1 through 50, or zero for YouTube's default of 5";
  if (size) query.maxResults = String(size);
  if (token) { if (token.length > 256 || !pageToken.test(token)) return 'pageToken must be a nextPageToken returned by YouTube'; query.pageToken = token; }
  return undefined;
}
function playlistQuery(input: ListPlaylistItemsInput): { query?: Record<string, string>; error?: string } {
  if (!playlistID.test(input.playlistId)) return { error: 'playlistId must be a YouTube playlist ID' };
  const query: Record<string, string> = { part: 'snippet,contentDetails', playlistId: input.playlistId };
  if (input.videoId) { if (!videoID.test(input.videoId)) return { error: 'videoId must be an 11-character YouTube video ID' }; query.videoId = input.videoId; }
  const error = validatePage(query, input.maxResults, input.pageToken);
  return error ? { error } : { query };
}
function playlistItem(value: unknown, playlist: string): PlaylistItem {
  const item = object(value), snippet = object(item.snippet ?? {}), resource = object(snippet.resourceId ?? {}), content = object(item.contentDetails ?? {});
  const id = field(item, 'id'), video = field(resource, 'videoId'), position = integer(snippet, 'position');
  if (!id || id.length > 128 || !pageToken.test(id)) throw new TypeError('invalid item ID');
  if (field(snippet, 'playlistId') !== playlist || field(resource, 'kind') !== 'youtube#video' || !videoID.test(video) || position < 0) throw new TypeError('invalid playlist item');
  const result: PlaylistItem = { id, playlistId: playlist, videoId: video, title: field(snippet, 'title'), position, addedAt: timestamp(field(snippet, 'publishedAt')) };
  const published = optionalTimestamp(field(content, 'videoPublishedAt'));
  if (published) result.videoPublishedAt = published;
  return result;
}
function playlistPage(response: YouTubeResponse, input: ListPlaylistItemsInput): PlaylistItemPage {
  const fields = pageFields(decoded(response));
  if (fields.items.length > 50) throw new TypeError('too many items');
  const items = fields.items.map(value => playlistItem(value, input.playlistId));
  if (input.videoId && items.some(item => item.videoId !== input.videoId)) throw new TypeError('wrong video');
  return { items, totalResults: fields.totalResults, ...(fields.nextPageToken ? { nextPageToken: fields.nextPageToken } : {}) };
}

/** Search one YouTube video page. */
export class SearchVideosOperation implements Query<SearchVideosInput, VideoSearchPage> {
  /** Bind a client. */ constructor(private readonly client: YouTubeClient) {}
  /** Return manifest definition. */ definition() { return SearchVideosDefinition; }
  /** Invoke the search query. */ async invoke(call: Call, input: SearchVideosInput): Promise<QueryAttempt<VideoSearchPage>> {
    const operation = 'searchVideos', query: Record<string, string> = { part: 'snippet', type: 'video' };
    if (!input.query?.trim() && !input.channelId) return defect(operation, zeroSearch, 'query or channelId must be set');
    if ([...(input.query ?? '')].length > 500) return defect(operation, zeroSearch, 'query must be valid UTF-8 of at most 500 characters');
    if (input.query?.trim()) query.q = input.query;
    if (input.channelId) { if (!channelID.test(input.channelId)) return defect(operation, zeroSearch, 'channelId must be a YouTube channel ID: UC followed by 22 characters'); query.channelId = input.channelId; }
    if (input.order) { if (!['relevance', 'date', 'viewCount', 'rating', 'title'].includes(input.order)) return defect(operation, zeroSearch, 'order must be relevance, date, viewCount, rating, or title'); query.order = input.order; }
    if (input.publishedAfter) query.publishedAfter = new Date(input.publishedAfter).toISOString().replace('.000Z', 'Z');
    const error = validatePage(query, input.maxResults, input.pageToken); if (error) return defect(operation, zeroSearch, error);
    let response: YouTubeResponse;
    try { response = await this.client.exchange(call, operation, '/search', query); } catch (cause) { return queryError(operation, cause, zeroSearch, call, input.channelId ?? ''); }
    try {
      const fields = pageFields(decoded(response));
      if (fields.items.length > Math.max(input.maxResults ?? 0, 5) && fields.items.length > 50) throw new TypeError('too many results');
      const results: VideoSearchResult[] = fields.items.map(value => { const item = object(value), id = object(item.id ?? {}), snippet = object(item.snippet ?? {}); if (field(id, 'kind') !== 'youtube#video' || !videoID.test(field(id, 'videoId')) || !channelID.test(field(snippet, 'channelId'))) throw new TypeError('invalid video'); return { videoId: field(id, 'videoId'), channelId: field(snippet, 'channelId'), channelTitle: field(snippet, 'channelTitle'), title: field(snippet, 'title'), description: field(snippet, 'description'), publishedAt: timestamp(field(snippet, 'publishedAt')) }; });
      return newQueryBranch('searched', { results, totalResults: fields.totalResults, ...(fields.nextPageToken ? { nextPageToken: fields.nextPageToken } : {}) }, receipt(call, input.channelId ?? '', response));
    } catch { return invalid(operation, zeroSearch, 'YouTube returned an incomplete search page', call, input.channelId ?? '', response); }
  }
}

/** Read one video. */
export class GetVideoOperation implements Query<GetVideoInput, Video> {
  /** Bind a client. */ constructor(private readonly client: YouTubeClient) {}
  /** Return manifest definition. */ definition() { return GetVideoDefinition; }
  /** Invoke the video query. */ async invoke(call: Call, input: GetVideoInput): Promise<QueryAttempt<Video>> {
    const operation = 'getVideo'; if (!videoID.test(input.videoId)) return defect(operation, zeroVideo, 'videoId must be an 11-character YouTube video ID');
    let response: YouTubeResponse;
    try { response = await this.client.exchange(call, operation, '/videos', { id: input.videoId, part: 'snippet,contentDetails,status,statistics' }); } catch (cause) { return queryError(operation, cause, zeroVideo, call, input.videoId, true); }
    let items: unknown[];
    try { const parsed = decoded(response); if (!Array.isArray(parsed.items)) throw new TypeError(); items = parsed.items; } catch { return invalid(operation, zeroVideo, 'YouTube returned a malformed video list', call, input.videoId, response); }
    if (!items.length) return newQueryBranch('notFound', zeroVideo, receipt(call, input.videoId, response), failure(operation, 'NOT_FOUND', 'no visible YouTube video has the ID'));
    try {
      if (items.length !== 1) throw new TypeError();
      const item = object(items[0]), snippet = object(item.snippet ?? {}), content = object(item.contentDetails ?? {}), status = object(item.status ?? {}), stats = object(item.statistics ?? {});
      if (field(item, 'id') !== input.videoId || !channelID.test(field(snippet, 'channelId'))) throw new TypeError();
      const result: Video = { id: input.videoId, channelId: field(snippet, 'channelId'), channelTitle: field(snippet, 'channelTitle'), title: field(snippet, 'title'), description: field(snippet, 'description'), publishedAt: timestamp(field(snippet, 'publishedAt')), privacyStatus: field(status, 'privacyStatus') };
      const duration = field(content, 'duration'); if (duration) result.duration = duration;
      for (const [source, destination] of [['viewCount', 'viewCount'], ['likeCount', 'likeCount'], ['commentCount', 'commentCount']] as const) { const count = optionalCount(stats, source); if (count !== undefined) result[destination] = count; }
      return newQueryBranch('found', result, receipt(call, input.videoId, response));
    } catch { return invalid(operation, zeroVideo, 'YouTube returned a different or invalid video', call, input.videoId, response); }
  }
}

/** Read one channel. */
export class GetChannelOperation implements Query<GetChannelInput, Channel> {
  /** Bind a client. */ constructor(private readonly client: YouTubeClient) {}
  /** Return manifest definition. */ definition() { return GetChannelDefinition; }
  /** Invoke the channel query. */ async invoke(call: Call, input: GetChannelInput): Promise<QueryAttempt<Channel>> {
    const operation = 'getChannel', query: Record<string, string> = { part: 'snippet,contentDetails,statistics' };
    let objectID = ''; let selectors = 0;
    if (input.channelId) { selectors++; if (!channelID.test(input.channelId)) return defect(operation, zeroChannel, 'channelId must be a YouTube channel ID: UC followed by 22 characters'); query.id = input.channelId; objectID = input.channelId; }
    if (input.handle) { selectors++; if (!handle.test(input.handle)) return defect(operation, zeroChannel, 'handle must be a YouTube handle of 3 to 30 letters, digits, periods, hyphens, or underscores'); objectID = `@${input.handle.replace(/^@/, '')}`; query.forHandle = objectID; }
    if (input.mine) { selectors++; query.mine = 'true'; }
    if (selectors !== 1) return defect(operation, zeroChannel, 'set exactly one of channelId, handle, and mine');
    let response: YouTubeResponse;
    try { response = await this.client.exchange(call, operation, '/channels', query); } catch (cause) { return queryError(operation, cause, zeroChannel, call, objectID, true); }
    try {
      const parsed = decoded(response), items = parsed.items === undefined || parsed.items === null ? [] : parsed.items;
      if (!Array.isArray(items)) throw new TypeError();
      if (!items.length) return newQueryBranch('notFound', zeroChannel, receipt(call, objectID, response), failure(operation, 'NOT_FOUND', 'no YouTube channel matches the request'));
      if (items.length !== 1) throw new TypeError();
      const item = object(items[0]), snippet = object(item.snippet ?? {}), content = object(item.contentDetails ?? {}), related = object(content.relatedPlaylists ?? {}), stats = object(item.statistics ?? {}), id = field(item, 'id'), uploads = field(related, 'uploads');
      if (!channelID.test(id) || (input.channelId && id !== input.channelId) || !playlistID.test(uploads)) throw new TypeError();
      const result: Channel = { id, title: field(snippet, 'title'), description: field(snippet, 'description'), publishedAt: timestamp(field(snippet, 'publishedAt')), uploadsPlaylistId: uploads };
      const custom = field(snippet, 'customUrl'); if (custom) result.customUrl = custom;
      const hidden = stats.hiddenSubscriberCount; if (hidden !== undefined && hidden !== null && typeof hidden !== 'boolean') throw new TypeError();
      if (!hidden) { const count = optionalCount(stats, 'subscriberCount'); if (count !== undefined) result.subscriberCount = count; }
      for (const key of ['videoCount', 'viewCount'] as const) { const count = optionalCount(stats, key); if (count !== undefined) result[key] = count; }
      return newQueryBranch('found', result, receipt(call, id, response));
    } catch { return invalid(operation, zeroChannel, 'YouTube returned a different channel or an invalid channel ID', call, objectID, response); }
  }
}

/** List one playlist page. */
export class ListPlaylistItemsOperation implements Query<ListPlaylistItemsInput, PlaylistItemPage> {
  /** Bind a client. */ constructor(private readonly client: YouTubeClient) {}
  /** Return manifest definition. */ definition() { return ListPlaylistItemsDefinition; }
  /** Invoke the playlist query. */ async invoke(call: Call, input: ListPlaylistItemsInput): Promise<QueryAttempt<PlaylistItemPage>> {
    const operation = 'listPlaylistItems', encoded = playlistQuery(input);
    if (encoded.error) return defect(operation, zeroPage, encoded.error);
    let response: YouTubeResponse;
    try { response = await this.client.exchange(call, operation, '/playlistItems', encoded.query!); } catch (cause) { return queryError(operation, cause, zeroPage, call, input.playlistId, true); }
    try { return newQueryBranch('listed', playlistPage(response, input), receipt(call, input.playlistId, response)); }
    catch { return invalid(operation, zeroPage, 'YouTube returned an incomplete playlist page', call, input.playlistId, response); }
  }
}

/** Add a video once after checking playlist membership. */
/**
 * Add a video at most once per Step call after checking the playlist for it. YouTube has no idempotency key
 * and its listing can lag behind an insert, so a single-dispatch fence forbids a second insert after one that
 * may have reached YouTube; such a later attempt only reads the playlist back.
 */
export class AddPlaylistItemOperation implements Mutation<AddPlaylistItemInput, AddedPlaylistItem> {
  /** Bind a client. */ constructor(private readonly client: YouTubeClient) {}
  /** Return manifest definition. */ definition() { return AddPlaylistItemDefinition; }
  /** Use the stable Call ID as the Dex idempotency key; YouTube does not accept it. */ idempotencyKey(callID: string) { return callID; }
  /** Check membership and insert at most once per Step call. */ async invoke(call: Call, input: AddPlaylistItemInput): Promise<MutationAttempt<AddedPlaylistItem>> {
    const operation = 'addPlaylistItem', listInput = { playlistId: input.playlistId, videoId: input.videoId, maxResults: 1 }, encoded = playlistQuery(listInput);
    if (encoded.error || !input.videoId) return newMutationBranch('defect', zeroAdded, zeroReceipt(), failure(operation, 'VALIDATION', encoded.error ?? 'videoId must be an 11-character YouTube video ID'));
    let fence: Fence;
    try { fence = new Fence(call, AddPlaylistItemDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000); }
    catch { return newMutationBranch('defect', zeroAdded, zeroReceipt(), failure(operation, 'LOCAL_DEFECT', 'the YouTube dispatch fence is unavailable')); }
    const decision = fence.decide();
    if (decision.kind === 'readBackRequired') return this.confirmEarlierInsert(call, listInput, encoded.query!);
    if (decision.kind !== 'sendAllowed') return newMutationUncertain(zeroAdded, failure(operation, 'TRANSPORT', 'the YouTube dispatch checkpoint is unreadable, so an earlier insert cannot be ruled out; check the playlist before adding the video again'), zeroReceipt());
    let response: YouTubeResponse;
    try { response = await this.client.exchange(call, operation, '/playlistItems', encoded.query!, true); }
    catch (cause) {
      if (cause instanceof YouTubeExchangeError && cause.disposition === 'retry') {
        try { await fence.recordNotSent(); } catch { /* A lost notSent marker makes the next attempt read back instead of inserting. */ }
      }
      return mutationError(operation, cause, call, input.playlistId);
    }
    let page: PlaylistItemPage;
    try { page = playlistPage(response, listInput); }
    catch { return newMutationBranch('invalidResponse', zeroAdded, receipt(call, input.playlistId, response), failure(operation, 'PROTOCOL', 'YouTube returned an incomplete playlist page')); }
    if (page.items?.length) return newMutationBranch('added', { item: page.items[0]!, wasAlreadyPresent: true }, receipt(call, page.items[0]!.id, response));
    const body = JSON.stringify({ snippet: { playlistId: input.playlistId, resourceId: { kind: 'youtube#video', videoId: input.videoId } } });
    try { response = await this.client.exchange(call, operation, '/playlistItems', { part: 'snippet,contentDetails' }, true, body, fence); }
    catch (cause) {
      if (cause instanceof YouTubeExchangeError && cause.disposition === 'retryUnprocessed') {
        try { await fence.recordCanResend(); } catch { /* A lost canResend marker makes the next attempt read back instead of inserting. */ }
        return newMutationRetry(failure(operation, cause.kind as Failure['kind'], cause.message), cause.retryAfterMs);
      }
      // The claim marker stays, so the next attempt only reads the playlist back.
      if (cause instanceof YouTubeExchangeError && cause.disposition === 'writeUnconfirmed') return newMutationRetry(failure(operation, cause.kind as Failure['kind'], cause.message));
      return mutationError(operation, cause, call, input.playlistId);
    }
    let inserted: Record<string, unknown>;
    try { inserted = decoded(response); } catch { return newMutationRetry(failure(operation, 'PROTOCOL', 'YouTube returned a malformed insert response; the next attempt reads the playlist back')); }
    let item: PlaylistItem;
    try {
      item = playlistItem(inserted, input.playlistId);
      if (item.videoId !== input.videoId) throw new TypeError('YouTube returned an item for a different video');
    } catch { return newMutationBranch('invalidResponse', zeroAdded, receipt(call, input.playlistId, response), failure(operation, 'PROTOCOL', 'YouTube returned an invalid or different inserted item')); }
    try { await fence.recordProviderId(item.id); } catch { /* A lost provider ID marker still forbids another insert. */ }
    return newMutationBranch('added', { item, wasAlreadyPresent: false }, receipt(call, item.id, response));
  }

  /** List the playlist after an attempt that may have inserted; only a shown item confirms it. */
  private async confirmEarlierInsert(call: Call, listInput: ListPlaylistItemsInput, query: Record<string, string>): Promise<MutationAttempt<AddedPlaylistItem>> {
    const operation = 'addPlaylistItem';
    let response: YouTubeResponse;
    try { response = await this.client.exchange(call, operation, '/playlistItems', query, true); }
    catch (cause) {
      if (!(cause instanceof YouTubeExchangeError)) throw cause;
      if (cause.disposition === 'retry') return newMutationRetry(failure(operation, cause.kind as Failure['kind'], cause.message), cause.retryAfterMs);
      const readBackReceipt = cause.response || cause.message.startsWith('addPlaylistItem requires') ? receipt(call, listInput.playlistId, cause.response) : zeroReceipt();
      return newMutationUncertain(zeroAdded, failure(operation, cause.kind as Failure['kind'], `an earlier attempt may have added the video, and the playlist could not be read back: ${cause.message}`), readBackReceipt);
    }
    let page: PlaylistItemPage;
    try { page = playlistPage(response, listInput); }
    catch { return newMutationUncertain(zeroAdded, failure(operation, 'PROTOCOL', 'an earlier attempt may have added the video, and the playlist could not be read back: YouTube returned an incomplete playlist page'), receipt(call, listInput.playlistId, response)); }
    if (page.items?.length) return newMutationBranch('added', { item: page.items[0]!, wasAlreadyPresent: true }, receipt(call, page.items[0]!.id, response));
    return newMutationUncertain(zeroAdded, failure(operation, 'TRANSPORT', 'an earlier attempt may have added the video, and the playlist listing does not show it yet; check the playlist before adding the video again'), receipt(call, listInput.playlistId, response));
  }
}
