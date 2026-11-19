// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListCampaignsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListCampaignsInput, ListCampaignsOutput } from './generated/models.js';

/** Optional provider transport for local tests. */
export interface SensorsDataClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Read campaign metadata from one Sensors Data project. */
export class SensorsDataClient {
  private readonly baseURL: string;
  private readonly projectName: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate project settings without contacting Sensors Data. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SensorsDataClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Sensors Data configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl);
    if (new URL(this.baseURL).pathname !== '/') throw new TypeError('Sensors Data base URL must be an origin');
    this.projectName = config.projectName;
    if (!this.projectName || this.projectName.length > 128 || !/^[A-Za-z0-9_-]+$/.test(this.projectName)) throw new TypeError('Sensors Data project name must be one ASCII identifier');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Sensors Data request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the campaign page Query. */ listCampaigns(): ListCampaignsOperation { return new ListCampaignsOperation(this); }

  /** Send one bounded campaign page request. */
  async list(call: Call, pageNum: number, pageSize: number): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try {
      const credential = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credential.apiKey instanceof SecretString ? credential.apiKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (apiKey.length !== 35 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${this.baseURL}/api/v3/analytics/v1/channel/campaigns/list`, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'api-key': apiKey, 'sensorsdata-project': this.projectName }, body: Buffer.from(JSON.stringify({ page_num: pageNum, page_size: pageSize })), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read one Sensors Data campaign page. */
export class ListCampaignsOperation implements Query<ListCampaignsInput, ListCampaignsOutput> {
  /** Bind the provider client. */ constructor(private readonly client: SensorsDataClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListCampaignsDefinition; }
  /** Classify one campaign page using the Go connector contract. */
  async invoke(call: Call, input: ListCampaignsInput): Promise<QueryAttempt<ListCampaignsOutput>> {
    const pageNum = input?.pageNum;
    const pageSize = input?.pageSize || 20;
    if (!Number.isSafeInteger(pageNum) || !Number.isSafeInteger(pageSize) || pageNum < 0 || pageNum > 100_000 || pageSize < 1 || pageSize > 1000) return branch('defect', 'VALIDATION');
    const exchange = await this.client.list(call, pageNum, pageSize);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isObject(wire) || typeof wire.code !== 'string' || !wire.code) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code !== 'SUCCESS') return branch('providerRejected', 'PROVIDER_REJECTION');
    const data = wire.data;
    if (!isObject(data) || !Number.isSafeInteger(data.total_page) || !Number.isSafeInteger(data.page_num) || !Number.isSafeInteger(data.page_size) || (data.total_page as number) < 0 || (data.page_num as number) < 0 || (data.page_size as number) < 1 || (data.page_size as number) > 1000) return branch('invalidResponse', 'PROTOCOL');
    const totalRows = typeof data.total_rows === 'string' && /^\d+$/.test(data.total_rows) ? Number(data.total_rows) : data.total_rows;
    const details = data.campaign_details ?? [];
    if (!Number.isSafeInteger(totalRows) || (totalRows as number) < 0 || !Array.isArray(details) || details.length > pageSize) return branch('invalidResponse', 'PROTOCOL');
    const campaigns: ListCampaignsOutput['campaigns'] = [];
    for (const item of details) {
      if (!isObject(item) || typeof item.campaign_name !== 'string' || !item.campaign_name || Buffer.byteLength(item.campaign_name) > 1024 || !Number.isSafeInteger(item.branch_num) || (item.branch_num as number) < 0 || typeof item.latest_use_time !== 'string' || Buffer.byteLength(item.latest_use_time) > 64) return branch('invalidResponse', 'PROTOCOL');
      campaigns.push({ name: item.campaign_name, linkCount: item.branch_num as number, latestUseTime: item.latest_use_time });
    }
    return newQueryBranch('listed', { campaigns, totalRows: totalRows as number, totalPages: data.total_page as number, pageNum: data.page_num as number, pageSize: data.page_size as number });
  }
}

function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'sensorsdata', operation: 'listCampaigns', kind, message: 'Sensors Data campaign query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListCampaignsOutput> { return newQueryBranch(name, { campaigns: [], totalRows: 0, totalPages: 0, pageNum: 0, pageSize: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListCampaignsOutput> { return newQueryRetry(failure(kind), delay); }
