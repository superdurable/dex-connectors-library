// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetPhotoDefinition, SearchPhotosDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetPhotoInput, Photo, PhotoPage, SearchPhotosInput } from './generated/models.js';

const emptyPhoto: Photo = { id: '', htmlUrl: '', regularUrl: '', photographerName: '', photographerURL: '' };
const emptyPage: PhotoPage = { total: 0, totalPages: 0, photos: [] };
const defaultBaseURL = 'https://api.unsplash.com';

/** Injectable provider transport for deterministic tests. */
export type UnsplashRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport and clock dependencies. */
export interface UnsplashClientOptions {
  /** HTTP sender; defaults to the SDK transport. */ send?: UnsplashRequestSender;
  /** Clock for HTTP Retry-After. */ now?: () => Date;
}

type Exchange = { response: ProviderResponse } | { branch: 'defect' | 'invalidResponse'; failure: Failure } | { retry: true; failure: Failure; retryAfterMs: number };
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'unsplash', operation, message }; }
function failed<Output>(operation: string, branch: string, output: Output, kind: Failure['kind'], message: string): QueryAttempt<Output> {
  return newQueryBranch(branch, output, {}, failure(operation, kind, message));
}

/** Authenticated Unsplash client; credentials are resolved for each call. */
export class Client {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: UnsplashRequestSender;
  private readonly now: () => Date;
  /** Validate settings and bind immutable dependencies. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: UnsplashClientOptions = {}) {
    if (!credentials) throw new TypeError('Unsplash credential source is required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 0) throw new TypeError('Unsplash configuration is invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind photo search. */ searchPhotos(): SearchPhotosOperation { return new SearchPhotosOperation(this); }
  /** Bind photo lookup. */ getPhoto(): GetPhotoOperation { return new GetPhotoOperation(this); }

  /** Make one provider request and classify transport and HTTP failures. */
  async get(call: Call, operation: string, path: string): Promise<Exchange> {
    let key: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      key = resolved.accessKey instanceof SecretString ? resolved.accessKey.reveal() : '';
    } catch { key = ''; }
    if (!isHeaderSafeCredential(key)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'Unsplash credentials are unavailable or invalid') };
    try {
      const response = await this.send({ url: `${this.baseURL}${path}`, method: 'GET', headers: { Authorization: `Client-ID ${key}`, Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseURL === defaultBaseURL ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal });
      return this.classify(operation, response);
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && error.status !== 200) return this.classify(operation, { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() });
        return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Unsplash response exceeded the limit') };
      }
      return { retry: true, failure: failure(operation, 'TRANSPORT', 'Unsplash response is unavailable'), retryAfterMs: 0 };
    }
  }
  private classify(operation: string, response: ProviderResponse): Exchange {
    if (response.status === 408 || response.status === 429 || response.status >= 500) {
      const raw = response.headers['retry-after'];
      return { retry: true, failure: failure(operation, 'AVAILABILITY', 'Unsplash is temporarily unavailable'), retryAfterMs: parseRetryAfter(Array.isArray(raw) ? raw[0] ?? '' : raw ?? '', this.now()) };
    }
    return { response };
  }
}

/** Search a page of public Unsplash photos. */
export class SearchPhotosOperation implements Query<SearchPhotosInput, PhotoPage> {
  /** Bind one authenticated client. */ constructor(private readonly client: Client) {}
  /** Return generated operation metadata. */ definition() { return SearchPhotosDefinition; }
  /** Validate, request, and classify one search attempt. */
  async invoke(call: Call, input: SearchPhotosInput): Promise<QueryAttempt<PhotoPage>> {
    if (!input.query?.trim() || !validCount(input.page, 0, Infinity) || !validCount(input.perPage, 0, 30)) return failed('searchPhotos', 'defect', emptyPage, 'VALIDATION', 'query and page size must be valid');
    const query = new URLSearchParams();
    if (input.page) query.set('page', String(input.page));
    if (input.perPage) query.set('per_page', String(input.perPage));
    query.set('query', input.query);
    const exchange = await this.client.get(call, 'searchPhotos', `/search/photos?${query.toString()}`);
    if ('retry' in exchange) return newQueryRetry(exchange.failure, exchange.retryAfterMs);
    if ('branch' in exchange) return newQueryBranch(exchange.branch, emptyPage, {}, exchange.failure);
    const { status, body } = exchange.response;
    if (status !== 200) return failed('searchPhotos', 'providerRejected', emptyPage, rejectionKind(status), `Unsplash rejected search with HTTP ${status}`);
    try {
      const wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown;
      if (!isObject(wire) || !Array.isArray(wire.results)) throw new Error('invalid page');
      const total = goInteger(wire.total), totalPages = goInteger(wire.total_pages);
      if (total < 0 || totalPages < 0) throw new Error('invalid page');
      return newQueryBranch('searched', { total, totalPages, photos: wire.results.map(decodePhoto) }, {});
    } catch { return failed('searchPhotos', 'invalidResponse', emptyPage, 'PROTOCOL', 'Unsplash returned an invalid search page'); }
  }
}

/** Retrieve one public Unsplash photo. */
export class GetPhotoOperation implements Query<GetPhotoInput, Photo> {
  /** Bind one authenticated client. */ constructor(private readonly client: Client) {}
  /** Return generated operation metadata. */ definition() { return GetPhotoDefinition; }
  /** Validate, request, and classify one lookup attempt. */
  async invoke(call: Call, input: GetPhotoInput): Promise<QueryAttempt<Photo>> {
    if (!input.id?.trim() || /[/?&#]/.test(input.id)) return failed('getPhoto', 'defect', emptyPhoto, 'VALIDATION', 'photo ID is invalid');
    const exchange = await this.client.get(call, 'getPhoto', `/photos/${encodeGoPath(input.id)}`);
    if ('retry' in exchange) return newQueryRetry(exchange.failure, exchange.retryAfterMs);
    if ('branch' in exchange) return newQueryBranch(exchange.branch, emptyPhoto, {}, exchange.failure);
    const { status, body } = exchange.response;
    if (status === 404) return failed('getPhoto', 'notFound', emptyPhoto, 'NOT_FOUND', 'Unsplash photo was not found');
    if (status !== 200) return failed('getPhoto', 'providerRejected', emptyPhoto, rejectionKind(status), `Unsplash rejected photo lookup with HTTP ${status}`);
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)); }
    catch { return failed('getPhoto', 'invalidResponse', emptyPhoto, 'PROTOCOL', 'Unsplash returned malformed photo JSON'); }
    try { return newQueryBranch('found', decodePhoto(wire), {}); }
    catch { return failed('getPhoto', 'invalidResponse', emptyPhoto, 'PROTOCOL', 'Unsplash returned an invalid photo'); }
  }
}

function validCount(value: number | undefined, minimum: number, maximum: number): boolean { return value === undefined || (Number.isSafeInteger(value) && value >= minimum && value <= maximum); }
function goInteger(value: unknown): number { if (value === undefined || value === null) return 0; if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new Error('integer expected'); return value; }
function isObject(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
function goString(value: unknown): string { if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new Error('string expected'); return value; }
function nested(value: unknown): Record<string, unknown> { if (value === undefined || value === null) return {}; if (!isObject(value)) throw new Error('object expected'); return value; }
function httpsURL(value: string): boolean { try { const parsed = new URL(value); return parsed.protocol === 'https:' && !!parsed.host; } catch { return false; } }
function decodePhoto(value: unknown): Photo {
  const wire = nested(value), urls = nested(wire.urls), links = nested(wire.links), user = nested(wire.user), userLinks = nested(user.links);
  const description = wire.description === null || wire.description === undefined ? undefined : goString(wire.description);
  const altDescription = wire.alt_description === null || wire.alt_description === undefined ? undefined : goString(wire.alt_description);
  const photo: Photo = { id: goString(wire.id), htmlUrl: goString(links.html), regularUrl: goString(urls.regular), photographerName: goString(user.name), photographerURL: goString(userLinks.html), ...(description !== undefined ? { description } : {}), ...(altDescription !== undefined ? { altDescription } : {}) };
  if (!photo.id || !httpsURL(photo.htmlUrl) || !httpsURL(photo.regularUrl) || !photo.photographerName || !httpsURL(photo.photographerURL)) throw new Error('invalid photo');
  return photo;
}
function rejectionKind(status: number): Failure['kind'] { return status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION'; }
function encodeGoPath(value: string): string { return encodeURIComponent(value).replace(/[!'()*,;?]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%24|%26|%2B|%3D|%3A|%40/g, code => String.fromCharCode(parseInt(code.slice(1), 16))); }
