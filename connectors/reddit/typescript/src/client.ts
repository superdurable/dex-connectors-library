// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, ReauthorizationRequiredError, resolveCredential, type Call, type CredentialProvider, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, MaxRetryAfterDelayMs, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials } from './generated/models.js';
import { GetPostOperation } from './operations.js';
import { ListSubredditPostsOperation, SearchPostsOperation } from './post_listings.js';

/** Injectable Reddit transport and OAuth refresh dependencies. */
export interface RedditClientOptions {
  /** Provider sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Refresh driver; omitted uses the manifest declaration. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Clock used for Retry-After dates. */ now?: () => Date;
}

/** One bounded provider response or a classified local failure. */
export interface RedditExchange {
  /** HTTP status, or zero before response headers. */ status: number;
  /** Response bytes retained only during this call. */ body: Uint8Array;
  /** Retry-After delay in milliseconds; on a 429 without Retry-After, the X-Ratelimit-Reset delay. */ retryAfterMs: number;
  /** Location header of a redirect, or empty. */ location: string;
  /** Safe local failure class. */ error?: 'credential' | 'refresh' | 'request' | 'tooLarge';
}

/** Reddit client resolving renewable OAuth credentials for each call. */
export class RedditClient {
  private readonly baseURL: string;
  private readonly redditUsername: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly refreshDriver: CredentialRefreshDriver<Credentials>;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate settings without contacting Reddit. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: RedditClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Reddit configuration and credentials are required');
    if (!/^[A-Za-z0-9_-]{3,20}$/.test(config.redditUsername)) throw new TypeError('Reddit username is invalid');
    this.redditUsername = config.redditUsername;
    this.baseURL = validateBaseURL(config.baseUrl || 'https://oauth.reddit.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 8_388_608;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Reddit request limits are invalid');
    this.refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Reddit', auth: Authorization, credentialFields: CredentialFields,
    });
    this.sendRequest = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the post Query. */ getPost(): GetPostOperation { return new GetPostOperation(this); }
  /** Bind the subreddit listing Query. */ listSubredditPosts(): ListSubredditPostsOperation { return new ListSubredditPostsOperation(this); }
  /** Bind the post search Query. */ searchPosts(): SearchPostsOperation { return new SearchPostsOperation(this); }

  /** Send one bounded GET with a bearer token held outside Flow state. */
  async get(call: Call, path: string): Promise<RedditExchange> {
    let token: string;
    let clientId: string;
    try {
      const resolved = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      token = resolved.accessToken instanceof SecretString ? resolved.accessToken.reveal() : '';
      clientId = resolved.oauthClientId;
      if (!token || !isHeaderSafeCredential(token) || !isHeaderSafeCredential(clientId)) return failedExchange('credential');
    } catch (error) {
      return failedExchange(error instanceof ReauthorizationRequiredError ? 'credential' : 'refresh');
    }
    const request: ProviderRequest = {
      url: this.baseURL + path, method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${token}`, 'User-Agent': `desktop:${clientId}:v0.1.0 (by /u/${this.redditUsername})` },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined)
        return { ...failedExchange('tooLarge'), status: error.status, retryAfterMs: this.retryDelayMs(error.status, error.headers ?? {}) };
      if (error instanceof ProviderTransportError) return failedExchange('request');
      return failedExchange('request');
    }
    const retryAfterMs = this.retryDelayMs(response.status, response.headers);
    if (Buffer.from(response.body).includes(token)) return { ...failedExchange('credential'), status: response.status, retryAfterMs };
    return { status: response.status, body: response.body, retryAfterMs, location: header(response.headers, 'location') };
  }

  private retryDelayMs(status: number, headers: ProviderResponse['headers']): number {
    const retryAfterMs = parseRetryAfter(header(headers, 'retry-after'), this.now());
    return status === 429 && retryAfterMs === 0 ? rateLimitResetDelayMs(header(headers, 'x-ratelimit-reset')) : retryAfterMs;
  }
}

/** Convert X-Ratelimit-Reset seconds to a Retry delay, rounded up and capped like Retry-After, as Go does. */
function rateLimitResetDelayMs(value: string): number {
  const seconds = parseGoFloat(trimGoSpace(value));
  if (seconds === undefined || Number.isNaN(seconds) || seconds <= 0) return 0;
  if (seconds >= MaxRetryAfterDelayMs / 1000) return MaxRetryAfterDelayMs;
  return Math.ceil(seconds) * 1000;
}

/** Parse Go strconv.ParseFloat syntax, returning undefined for a syntax or range error. */
function parseGoFloat(text: string): number | undefined {
  const special = /^([+-]?)(inf|infinity|nan)$/i.exec(text);
  if (special) return special[2]?.toLowerCase() === 'nan' ? Number.NaN : special[1] === '-' ? Number.NEGATIVE_INFINITY : Number.POSITIVE_INFINITY;
  const digits = '[0-9](?:_?[0-9])*';
  const hexDigits = '[0-9A-Fa-f](?:_?[0-9A-Fa-f])*';
  let value: number;
  if (new RegExp(`^[+-]?(?:${digits}(?:\\.(?:${digits})?)?|\\.${digits})(?:[eE][+-]?${digits})?$`).test(text)) {
    value = Number(text.replace(/_/g, ''));
  } else {
    const hex = new RegExp(`^([+-]?)0[xX](?:_?(${hexDigits})(?:\\.(${hexDigits})?)?|\\.(${hexDigits}))[pP]([+-]?${digits})$`).exec(text);
    if (!hex) return undefined;
    const [, sign = '', whole = '', fraction = '', fractionOnly = ''] = hex;
    const fractionDigits = (fraction || fractionOnly).replace(/_/g, '');
    const mantissa = (whole ? parseInt(whole.replace(/_/g, ''), 16) : 0) + (fractionDigits ? parseInt(fractionDigits, 16) / 16 ** fractionDigits.length : 0);
    value = (sign === '-' ? -1 : 1) * mantissa * 2 ** Number((hex[5] ?? '0').replace(/_/g, ''));
  }
  return Number.isFinite(value) ? value : undefined;
}

// Go strings.TrimSpace removes exactly the unicode.IsSpace code points at both ends.
function trimGoSpace(value: string): string {
  const characters = Array.from(value);
  let start = 0, end = characters.length;
  while (start < end && isGoSpace(characters[start]?.codePointAt(0) ?? 0)) start++;
  while (end > start && isGoSpace(characters[end - 1]?.codePointAt(0) ?? 0)) end--;
  return characters.slice(start, end).join('');
}

/** Report whether a code point is space to Go's unicode.IsSpace. */
export function isGoSpace(codePoint: number): boolean {
  return (codePoint >= 0x09 && codePoint <= 0x0d) || codePoint === 0x20 || codePoint === 0x85 || codePoint === 0xa0 ||
    codePoint === 0x1680 || (codePoint >= 0x2000 && codePoint <= 0x200a) || codePoint === 0x2028 || codePoint === 0x2029 ||
    codePoint === 0x202f || codePoint === 0x205f || codePoint === 0x3000;
}

function failedExchange(error: NonNullable<RedditExchange['error']>): RedditExchange {
  return { status: 0, body: new Uint8Array(), retryAfterMs: 0, location: '', error };
}
function header(headers: ProviderResponse['headers'], name: string): string {
  const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
