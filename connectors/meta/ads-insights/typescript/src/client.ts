// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAdDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetAdInput, AdMetadata } from './generated/models.js';

const emptyAd = (): AdMetadata => ({ id: '', name: '', status: '', effectiveStatus: '' });
const numericID = /^[0-9]{1,40}$/;

/** Optional provider transport for local tests and examples. */
export interface MetaAdsInsightsClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated Marketing API client with call-time token resolution. */
export class MetaAdsInsightsClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate configuration without contacting Meta. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: MetaAdsInsightsClientOptions = {}) {
    if (!credentials) throw new TypeError('Meta Ads Insights credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://graph.facebook.com/v25.0');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Meta timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Meta response limit is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the ad Query. */ getAd(): GetAdOperation { return new GetAdOperation(this); }
  /** Fetch one provider response and classify transport and HTTP failures. */
  async exchange(call: Call, operation: string, path: string): Promise<ProviderOutcome> {
    let accessToken: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      accessToken = resolved.userAccessToken instanceof SecretString ? resolved.userAccessToken.reveal() : '';
    } catch { return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') }; }
    if (!accessToken || !isHeaderSafeCredential(accessToken)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${accessToken}`, 'User-Agent': 'Dex Connector (https://superdurable.io)' },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Meta response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'Meta request failed') };
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(headerValue(response.headers['retry-after']));
    if (Buffer.from(response.body).includes(accessToken)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    if (status === 429) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Meta rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Meta is temporarily unavailable') };
    const graphError = status >= 400 ? decodeGraphError(response.body) : undefined;
    if (graphError?.isThrottled) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Meta rate limited the request') };
    if (graphError?.isTransient) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Meta reported a transient error') };
    if (status === 404) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'Meta did not find the resource') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `Meta rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Meta response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** Read one ad's identity and delivery status. */
export class GetAdOperation implements Query<GetAdInput, AdMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: MetaAdsInsightsClient) {}
  /** Return the manifest-generated contract. */ definition(): QueryDefinition { return GetAdDefinition; }
  /** Classify ad lookups consistently with Go. */
  async invoke(call: Call, input: GetAdInput): Promise<QueryAttempt<AdMetadata>> {
    if (!input || typeof input.adId !== 'string' || !numericID.test(input.adId))
      return terminal('getAd', 'defect', 'VALIDATION', 'ad ID is invalid', emptyAd());
    const query = new URLSearchParams({ fields: 'id,name,status,effective_status' });
    const result = await this.client.exchange(call, 'getAd', `/${input.adId}?${query}`);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyAd(), {}, result.failure);
    const ad = projectAd(parseJSON(result.body!), input.adId);
    if (!ad) return invalid('getAd', emptyAd());
    return newQueryBranch('found', ad);
  }
}

function projectAd(value: unknown, requestedID: string): AdMetadata | undefined {
  if (!isObject(value) || value.id !== requestedID) return undefined;
  const name = value.name;
  const status = value.status;
  const effectiveStatus = value.effective_status;
  if (typeof name !== 'string' || !name || typeof status !== 'string' || !status || typeof effectiveStatus !== 'string' || !effectiveStatus) return undefined;
  return { id: requestedID, name, status, effectiveStatus };
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'meta-ads-insights', operation, kind, message }; }
function terminal<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function invalid<Output>(operation: string, empty: Output): QueryAttempt<Output> { return terminal(operation, 'invalidResponse', 'PROTOCOL', 'Meta returned invalid JSON', empty); }
/** Decode Meta's Graph API error envelope; undefined means the body is not one. */
function decodeGraphError(body: Uint8Array): { isThrottled: boolean; isTransient: boolean } | undefined {
  const document = parseJSON(body);
  if (!isObject(document) || !isObject(document.error)) return undefined;
  const code = document.error.code ?? 0;
  const isTransient = document.error.is_transient ?? false;
  if (!Number.isSafeInteger(code) || typeof isTransient !== 'boolean') return undefined;
  const numericCode = code as number;
  // Meta documents these rate-limit codes, which Graph returns with HTTP 4xx.
  const isThrottled = [4, 17, 32, 341, 613].includes(numericCode) || (numericCode >= 80_000 && numericCode <= 80_014);
  return { isThrottled, isTransient };
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
