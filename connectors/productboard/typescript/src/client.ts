// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListFeaturesDefinition } from './generated/definitions.js';
import type { Config, Credentials, FeatureMetadata, FeaturesPage, ListFeaturesInput } from './generated/models.js';

const defaultBaseURL = 'https://api.productboard.com/v2';
const emptyPage = (): FeaturesPage => ({ features: [], nextCursor: '' });

/** Injectable provider sender for tests and local applications. */
export interface ProductboardClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Productboard REST API v2 client for feature pages. */
export class ProductboardClient {
  /** Validated provider base URL for requests and next-page links. */ readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Productboard. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ProductboardClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Productboard configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Productboard request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the listFeatures Query. */ listFeatures(): ListFeaturesOperation { return new ListFeaturesOperation(this); }

  /** Resolve the token and send one bounded feature-page request. */
  async get(call: Call, pageCursor: string): Promise<Exchange> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = credentials.apiToken instanceof SecretString ? credentials.apiToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!token || !isHeaderSafeCredential(token)) return emptyExchange('credential');
    const url = new URL(`${this.baseURL}/entities`);
    url.searchParams.set('fields[]', 'name');
    url.searchParams.set('type[]', 'feature');
    if (pageCursor) url.searchParams.set('pageCursor', pageCursor);
    const request: ProviderRequest = {
      url: url.toString(), method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${token}` },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    let isTooLarge = false;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        isTooLarge = true;
      } else return emptyExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (isTooLarge || response.body.length > this.maxResponseBytes)
      return { status: response.status, body: new Uint8Array(), retryAfterMs, error: 'tooLarge' };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Read one feature page with IDs, names, and the next opaque cursor. */
export class ListFeaturesOperation implements Query<ListFeaturesInput, FeaturesPage> {
  /** Bind the provider client. */ constructor(private readonly client: ProductboardClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListFeaturesDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: ListFeaturesInput): Promise<QueryAttempt<FeaturesPage>> {
    const pageCursor = input?.pageCursor ?? '';
    if (!input || !isValidCursor(pageCursor))
      return terminal('defect', 'VALIDATION', 'page cursor contains invalid characters or exceeds 2048 bytes');
    const exchange = await this.client.get(call, pageCursor);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Productboard credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Productboard request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Productboard rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Productboard is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Productboard rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Productboard response exceeded the configured limit');
    const page = decodePage(exchange.body, this.client.baseURL);
    if (!page) return terminal('invalidResponse', 'PROTOCOL', 'Productboard returned invalid feature JSON');
    return newQueryBranch('listed', page);
  }
}

function isValidCursor(value: unknown): value is string {
  if (typeof value !== 'string' || Buffer.byteLength(value, 'utf8') > 2048) return false;
  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0;
    if (codePoint < 32 || (codePoint >= 127 && codePoint <= 159)) return false;
  }
  return true;
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'productboard', operation: 'listFeatures', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<FeaturesPage> { return newQueryBranch(branch, emptyPage(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<FeaturesPage> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
/** Accept the public entities URL or the entities URL below the configured base. */
function isNextPageURL(nextURL: URL, baseURL: string): boolean {
  if (nextURL.protocol === 'https:' && nextURL.host === 'api.productboard.com' && nextURL.pathname === '/v2/entities') return true;
  return `${nextURL.protocol}//${nextURL.host}${nextURL.pathname}` === `${baseURL.replace(/\/+$/, '')}/entities`;
}
function decodePage(body: Uint8Array, baseURL: string): FeaturesPage | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !Array.isArray(raw.data)) return undefined;
  const features: FeatureMetadata[] = [];
  for (const entity of raw.data) {
    if (!isObject(entity) || typeof entity.id !== 'string' || !entity.id || entity.type !== 'feature' ||
      !isObject(entity.fields) || typeof entity.fields.name !== 'string' || !entity.fields.name.trim()) return undefined;
    features.push({ id: entity.id, name: entity.fields.name });
  }
  const links = raw.links === undefined ? {} : raw.links;
  if (!isObject(links) || (links.next !== undefined && typeof links.next !== 'string')) return undefined;
  let nextCursor = '';
  if (links.next) {
    let nextURL: URL;
    try { nextURL = new URL(links.next); } catch { return undefined; }
    if (!isNextPageURL(nextURL, baseURL)) return undefined;
    const cursors = nextURL.searchParams.getAll('pageCursor');
    if (cursors.length !== 1 || !cursors[0] || !isValidCursor(cursors[0])) return undefined;
    nextCursor = cursors[0];
  }
  return { features, nextCursor };
}
