// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { ReauthorizationRequiredError, resolveCredential, resolveCredentialAfterRejection, type Call } from '@superdurable/connectors-sdk';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { SearchVideosOperation, GetVideoOperation, GetChannelOperation, ListPlaylistItemsOperation, AddPlaylistItemOperation } from './operations.js';

const defaultEndpoint = 'https://www.googleapis.com/youtube/v3';
const defaultResponseBytes = 4_194_304;
const defaultTimeoutMs = 25_000;

/** Inject a provider transport for deterministic tests. */
export interface YouTubeClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; now?: () => Date; }
/** One provider exchange. */
export interface YouTubeResponse { status: number; headers: ProviderResponse['headers']; body: Uint8Array; isTooLarge: boolean; }
/** One classified provider exchange. */
export class YouTubeExchangeError extends Error {
  constructor(readonly disposition: 'retry' | 'quotaExhausted' | 'notFound' | 'providerRejected' | 'invalidResponse' | 'defect', readonly kind: string, message: string, readonly response?: YouTubeResponse, readonly retryAfterMs = 0) { super(message); }
}

/** YouTube API client using the generated OAuth refresh declaration. */
export class YouTubeClient {
  readonly endpoint: string;
  readonly maxResponseBytes: number;
  readonly timeoutMs: number;
  readonly now: () => Date;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'YouTube', auth: Authorization, credentialFields: CredentialFields });

  /** Validate the connection and bind host-owned renewable credentials. */
  constructor(config: Config & { timeoutMs?: number }, readonly credentials: CredentialSource, options: YouTubeClientOptions = {}) {
    if (!credentials) throw new TypeError('YouTube credential source is required');
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint);
    this.maxResponseBytes = config.maxResponseBytes || defaultResponseBytes;
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('YouTube maxResponseBytes must be positive');
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('YouTube timeout must be positive');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the search query. */ searchVideos() { return new SearchVideosOperation(this); }
  /** Bind the video query. */ getVideo() { return new GetVideoOperation(this); }
  /** Bind the channel query. */ getChannel() { return new GetChannelOperation(this); }
  /** Bind the playlist query. */ listPlaylistItems() { return new ListPlaylistItemsOperation(this); }
  /** Bind the playlist mutation. */ addPlaylistItem() { return new AddPlaylistItemOperation(this); }

  /** Send one request, resolving and refreshing credentials as the host requires. */
  async exchange(call: Call, operation: string, path: string, query: Record<string, string>, requiresManage = false, body?: string): Promise<YouTubeResponse> {
    let credentials: Credentials;
    try { credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal); }
    catch (error) {
      if (error instanceof ReauthorizationRequiredError) throw new YouTubeExchangeError('providerRejected', 'AUTHENTICATION', 'the YouTube connection must be authorized again');
      throw new YouTubeExchangeError('defect', 'AUTHENTICATION', 'YouTube connection credentials are unavailable or invalid');
    }
    if (!credentials.accessToken || !isHeaderSafeCredential(credentials.accessToken.reveal())) throw new YouTubeExchangeError('defect', 'AUTHENTICATION', 'YouTube connection credentials are unavailable or invalid');
    if (requiresManage && credentials.authMethodId !== 'google-oauth-manage') throw new YouTubeExchangeError('providerRejected', 'AUTHORIZATION', 'addPlaylistItem requires a YouTube connection authorized with the google-oauth-manage method and its youtube scope');
    const encoded = Object.keys(query).sort().map(key => `${goQueryEscape(key)}=${goQueryEscape(query[key]!)}`).join('&');
    const url = `${this.endpoint}${path}${encoded ? `?${encoded}` : ''}`;
    let response = await this.request(call, url, credentials.accessToken.reveal(), body);
    if (response.status === 401 && 'resolveAfterRejection' in this.credentials) {
      try {
        credentials = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
        response = await this.request(call, url, credentials.accessToken.reveal(), body);
      } catch (error) {
        if (error instanceof YouTubeExchangeError) throw error;
        if (error instanceof ReauthorizationRequiredError) throw new YouTubeExchangeError('providerRejected', 'AUTHENTICATION', 'the YouTube connection must be authorized again');
        throw new YouTubeExchangeError('defect', 'AUTHENTICATION', 'YouTube connection credentials are unavailable or invalid');
      }
    }
    if (response.isTooLarge) throw new YouTubeExchangeError('invalidResponse', 'RESPONSE_TOO_LARGE', 'YouTube response exceeds the configured size limit', response);
    if (response.status >= 200 && response.status < 300) return response;
    throw classifyError(response, operation, this.now());
  }

  private async request(call: Call, url: string, token: string, body?: string): Promise<YouTubeResponse> {
    const request: ProviderRequest = { url, method: body === undefined ? 'GET' : 'POST', headers: { Accept: 'application/json', Authorization: `Bearer ${token}`, ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) }, ...(body === undefined ? {} : { body: new TextEncoder().encode(body) }), addressPolicy: this.endpoint.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal };
    try { return { ...(await this.send(request)), isTooLarge: false }; }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined && error.headers !== undefined) {
        if (error.status < 200 || error.status >= 300) return { status: error.status, headers: error.headers, body: new Uint8Array(), isTooLarge: false };
        return { status: error.status, headers: error.headers, body: new Uint8Array(), isTooLarge: true };
      }
      throw new YouTubeExchangeError('retry', 'TRANSPORT', 'YouTube did not return a complete response');
    }
  }
}

/** Encode one query component exactly as Go url.QueryEscape. */
export function goQueryEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*~]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%20/g, '+'); }

function header(response: YouTubeResponse, name: string): string {
  const value = response.headers[name.toLowerCase()];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
const documentedReasons = new Set(['quotaExceeded', 'dailyLimitExceeded', 'rateLimitExceeded', 'userRateLimitExceeded', 'forbidden', 'insufficientPermissions', 'authError', 'invalidCredentials', 'badRequest', 'invalidParameter', 'videoNotFound', 'playlistNotFound', 'channelNotFound', 'playlistItemsNotAccessible', 'playlistContainsMaximumNumberOfVideos', 'manualSortRequired', 'videoAlreadyInPlaylist', 'backendError', 'notFound', 'youtubeSignupRequired']);
function classifyError(response: YouTubeResponse, _operation: string, now: Date): YouTubeExchangeError {
  let reason = '';
  try { const parsed = JSON.parse(new TextDecoder().decode(response.body)) as { error?: { errors?: { reason?: unknown }[] } }; const candidate = parsed.error?.errors?.[0]?.reason; if (typeof candidate === 'string' && documentedReasons.has(candidate)) reason = candidate; } catch { /* An unreadable error body cannot alter status classification. */ }
  const detail = reason ? `, ${reason}` : '';
  const status = response.status;
  const retryAfterMs = parseRetryAfter(header(response, 'retry-after'), now);
  if (reason === 'quotaExceeded' || reason === 'dailyLimitExceeded') return new YouTubeExchangeError('quotaExhausted', 'QUOTA_EXHAUSTED', `the project's daily YouTube Data API quota is spent (HTTP ${status}${detail})`, response);
  if (status === 429 || reason === 'rateLimitExceeded' || reason === 'userRateLimitExceeded') return new YouTubeExchangeError('retry', 'RATE_LIMIT', `YouTube rate limited the request (HTTP ${status}${detail})`, response, retryAfterMs);
  if (status === 408 || (status >= 500 && status !== 501)) return new YouTubeExchangeError('retry', 'AVAILABILITY', `YouTube is temporarily unavailable (HTTP ${status}${detail})`, response, retryAfterMs);
  if (status === 401) return new YouTubeExchangeError('providerRejected', 'AUTHENTICATION', `Google rejected the access token after one refresh (HTTP 401${detail})`, response);
  if (status === 404) return new YouTubeExchangeError('notFound', 'NOT_FOUND', `YouTube found no such video, channel, or playlist (HTTP 404${detail})`, response);
  if (status === 403) return new YouTubeExchangeError('providerRejected', 'AUTHORIZATION', `YouTube denied access: check the youtube scope and that the account owns the playlist (HTTP 403${detail})`, response);
  if (status === 400 || status === 409) return new YouTubeExchangeError('providerRejected', 'VALIDATION', `YouTube rejected the request as invalid (HTTP ${status}${detail})`, response);
  if (status >= 300 && status < 400) return new YouTubeExchangeError('providerRejected', 'PROTOCOL', `YouTube redirected the request (HTTP ${status})`, response);
  return new YouTubeExchangeError('providerRejected', 'PROVIDER_REJECTION', `YouTube rejected the request (HTTP ${status}${detail})`, response);
}
