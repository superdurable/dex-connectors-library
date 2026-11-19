// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListUserVideosDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListUserVideosInput, UserVideo, UserVideosPage } from './generated/models.js';

const emptyOutput: UserVideosPage = { videos: [], nextCursor: '' };
/** Send a Kuaishou request through a caller-owned transport. */
export type VideosRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport dependency for provider tests. */
export interface KuaishouClientOptions { /** Request sender; production uses the Connector SDK transport. */ send?: VideosRequestSender; }
/** Resolve Kuaishou credentials for each video-list query. */
export class KuaishouClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly pageSize: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: VideosRequestSender;
  /** Validate configuration without contacting Kuaishou. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: KuaishouClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Kuaishou configuration and credential source are required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://open.kuaishou.com').replace(/\/$/, '');
    if (new URL(this.baseUrl).pathname !== '/' || new URL(this.baseUrl).search || new URL(this.baseUrl).hash) throw new TypeError('Kuaishou API URL must be an origin');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    this.pageSize = config.pageSize ?? 20;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || !Number.isInteger(this.pageSize) || this.pageSize < 1 || this.pageSize > 200) throw new TypeError('Kuaishou configuration is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the video-list Query. */
  listUserVideos(): ListUserVideosOperation { return new ListUserVideosOperation(this); }
}
/** Read one page of the authorized user's videos. */
export class ListUserVideosOperation implements Query<ListUserVideosInput, UserVideosPage> {
  /** Bind an authenticated Kuaishou client. */
  constructor(private readonly client: KuaishouClient) {}
  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return ListUserVideosDefinition; }
  /** Send and classify one video-list request. */
  async invoke(call: Call, input: ListUserVideosInput): Promise<QueryAttempt<UserVideosPage>> {
    if (input.cursor && !isOpaque(input.cursor, 128)) return branch('defect', 'VALIDATION', 'Kuaishou cursor is invalid');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'Kuaishou credentials are unavailable or invalid'); }
    const token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    if (!isOpaque(credentials.appID ?? '', 4096) || !isOpaque(token, 4096)) return branch('defect', 'AUTHENTICATION', 'Kuaishou credentials are unavailable or invalid');
    // The Open Platform requires access_token in the query string.
    const query = new URLSearchParams({ app_id: credentials.appID, access_token: token, count: String(this.client.pageSize) });
    if (input.cursor) query.set('cursor', input.cursor);
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: `${this.client.baseUrl}/openapi/photo/list?${query.toString()}`, method: 'GET',
        headers: { Accept: 'application/json' }, timeoutMs: this.client.timeoutMs,
        maxResponseBytes: this.client.maxResponseBytes, addressPolicy: this.client.baseUrl === 'https://open.kuaishou.com' ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429 || error.status === 408 || error.status >= 500) return retry(error.status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', 'Kuaishou is temporarily unavailable', getRetryAfter(error.headers ?? {}));
        return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Kuaishou response exceeded the configured limit');
      }
      return retry('TRANSPORT', 'Kuaishou request failed');
    }
    const delay = getRetryAfter(response.headers);
    if (response.status === 429) return retry('RATE_LIMIT', 'Kuaishou rate limited the list query', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', 'Kuaishou is temporarily unavailable', delay);
    if (response.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `Kuaishou rejected HTTP ${response.status}`);
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Kuaishou returned invalid video JSON'); }
    if (!isRecord(document) || !Number.isInteger(document.result)) return branch('invalidResponse', 'PROTOCOL', 'Kuaishou returned no result code');
    if (document.result !== 1) return branch('providerRejected', 'PROVIDER_REJECTION', `Kuaishou rejected code ${document.result}`);
    if (!Array.isArray(document.video_list) || document.video_list.length > this.client.pageSize) return branch('invalidResponse', 'PROTOCOL', 'Kuaishou returned incomplete or oversized video list');
    const videos: UserVideo[] = [];
    let nextCursor = '';
    for (const item of document.video_list as unknown[]) {
      if (!isRecord(item) || !isOpaque(item.photo_id, 128) || typeof item.caption !== 'string' || !isNonnegativeInteger(item.create_time) || !isNonnegativeInteger(item.like_count) || !isNonnegativeInteger(item.comment_count) || !isNonnegativeInteger(item.view_count) || typeof item.pending !== 'boolean') return branch('invalidResponse', 'PROTOCOL', 'Kuaishou returned invalid video metadata');
      videos.push({ photoID: item.photo_id, caption: item.caption, createdAtMilliseconds: item.create_time, likeCount: item.like_count, commentCount: item.comment_count, viewCount: item.view_count, pending: item.pending });
      nextCursor = item.photo_id;
    }
    return newQueryBranch('listed', { videos, nextCursor });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isOpaque(value: unknown, maxLength: number): value is string { return typeof value === 'string' && value.length > 0 && value.length <= maxLength && /^[!-~]+$/.test(value); }
function isNonnegativeInteger(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0; }
function getRetryAfter(headers: ProviderResponse['headers']): number { const value = headers['retry-after']; return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? ''); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'kuaishou', operation: 'listUserVideos', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<UserVideosPage> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<UserVideosPage> { return newQueryRetry(failure(kind, message), delayMs); }
