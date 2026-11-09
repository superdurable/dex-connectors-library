// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListCampaignsDefinition } from './generated/definitions.js';
import type { Config, Credentials, CampaignMetadata, CampaignsPage, ListCampaignsInput } from './generated/models.js';

const defaultBaseURL = 'https://business-api.tiktok.com/open_api/v1.3';
const emptyPage = (): CampaignsPage => ({ campaigns: [], page: 0, totalPages: 0 });

/** Injectable provider sender for tests and local applications. */
export interface TikTokClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated TikTok Marketing API v1.3 client for campaign pages. */
export class TikTokClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting TikTok. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: TikTokClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('TikTok configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('TikTok request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the listCampaigns Query. */ listCampaigns(): ListCampaignsOperation { return new ListCampaignsOperation(this); }

  /** Resolve the token and send one bounded campaign page request. */
  async get(call: Call, advertiserID: string, page: number, pageSize: number): Promise<Exchange> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!token || !isHeaderSafeCredential(token)) return emptyExchange('credential');
    const url = new URL(`${this.baseURL}/campaign/get/`);
    url.searchParams.set('advertiser_id', advertiserID);
    url.searchParams.set('page', String(page));
    url.searchParams.set('page_size', String(pageSize));
    const request: ProviderRequest = {
      url: url.toString(), method: 'GET',
      headers: { Accept: 'application/json', 'Access-Token': token },
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
    if (Buffer.from(response.body).includes(token))
      return { ...emptyExchange('credential'), status: response.status, retryAfterMs };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Read one campaign page with IDs, names, and page metadata. */
export class ListCampaignsOperation implements Query<ListCampaignsInput, CampaignsPage> {
  /** Bind the provider client. */ constructor(private readonly client: TikTokClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListCampaignsDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: ListCampaignsInput): Promise<QueryAttempt<CampaignsPage>> {
    const page = input?.page ?? 0;
    const pageSize = input?.pageSize ?? 0;
    if (!isDecimalID(input?.advertiserId) || !Number.isSafeInteger(page) || page < 0 || page > 1_000_000 ||
      !Number.isSafeInteger(pageSize) || pageSize < 0 || pageSize > 1000)
      return terminal('defect', 'VALIDATION', 'advertiser ID or pagination is invalid');
    const selectedPage = page || 1;
    const selectedPageSize = pageSize || 10;
    const exchange = await this.client.get(call, input.advertiserId, selectedPage, selectedPageSize);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'TikTok credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'TikTok request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'TikTok rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'TikTok is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `TikTok rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'TikTok response exceeded the configured limit');
    const decoded = decodePage(exchange.body, selectedPage, selectedPageSize);
    if (!decoded) return terminal('invalidResponse', 'PROTOCOL', 'TikTok returned invalid campaign JSON');
    if (decoded.apiCode === tiktokCodeRateLimited) return retry('RATE_LIMIT', 'TikTok rate limited the request', exchange.retryAfterMs);
    if (isTikTokSystemErrorCode(decoded.apiCode)) return retry('AVAILABILITY', 'TikTok is temporarily unavailable', exchange.retryAfterMs);
    if (decoded.apiCode !== 0) return terminal('providerRejected', 'PROVIDER_REJECTION', `TikTok rejected the campaign query with code ${decoded.apiCode}`);
    return newQueryBranch('listed', decoded.page);
  }
}

// TikTok reports API outcomes in the body code of an HTTP 200 response.
const tiktokCodeRateLimited = 40100;
function isTikTokSystemErrorCode(code: number): boolean { return code >= 50000 && code <= 59999; }
function isDecimalID(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9][0-9]{0,19}$/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'tiktok-business', operation: 'listCampaigns', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<CampaignsPage> { return newQueryBranch(branch, emptyPage(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<CampaignsPage> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
/** Return the page for body code zero, otherwise the nonzero API code. */
function decodePage(body: Uint8Array, requestedPage: number, pageSize: number): { page: CampaignsPage; apiCode: number } | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !Number.isSafeInteger(raw.code)) return undefined;
  if (raw.code !== 0) return { page: emptyPage(), apiCode: raw.code as number };
  if (!isObject(raw.data) || !Array.isArray(raw.data.list) || !isObject(raw.data.page_info) ||
    raw.data.list.length > pageSize || raw.data.page_info.page !== requestedPage ||
    !Number.isSafeInteger(raw.data.page_info.total_page) ||
    (raw.data.page_info.total_page as number) < 0 || (raw.data.page_info.total_page as number) > 1_000_000) return undefined;
  const campaigns: CampaignMetadata[] = [];
  const seen = new Set<string>();
  for (const item of raw.data.list) {
    if (!isObject(item) || !isDecimalID(item.campaign_id) || typeof item.campaign_name !== 'string' ||
      !item.campaign_name.trim() || seen.has(item.campaign_id)) return undefined;
    seen.add(item.campaign_id);
    campaigns.push({ id: item.campaign_id, name: item.campaign_name });
  }
  return { page: { campaigns, page: requestedPage, totalPages: raw.data.page_info.total_page as number }, apiCode: 0 };
}
