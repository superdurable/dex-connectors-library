// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListDashboardsPageDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListDashboardsPageInput, ListDashboardsPageOutput } from './generated/models.js';

const dashboardsEndpoint = 'https://openapi.guance.com/api/v1/dashboards/list';

/** Optional provider sender for local tests. */
export interface GuanceClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** List dashboard identity and names available to the Guance API key. */
export class GuanceClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local settings without contacting Guance. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: GuanceClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Guance configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Guance request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the dashboard page Query. */ listDashboardsPage(): ListDashboardsPageOperation { return new ListDashboardsPageOperation(this); }

  /** Send one bounded dashboard page request. */
  async list(call: Call, pageIndex: number, pageSize: number): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey || apiKey.length > 1024 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const query = new URLSearchParams({ pageIndex: String(pageIndex), pageSize: String(pageSize) });
    const request: ProviderRequest = { url: dashboardsEndpoint + '?' + query.toString(), method: 'GET', headers: { 'DF-API-KEY': apiKey, Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read one Guance dashboard page. */
export class ListDashboardsPageOperation implements Query<ListDashboardsPageInput, ListDashboardsPageOutput> {
  /** Bind the provider client. */ constructor(private readonly client: GuanceClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListDashboardsPageDefinition; }
  /** Classify one page using the Go connector contract. */
  async invoke(call: Call, input: ListDashboardsPageInput): Promise<QueryAttempt<ListDashboardsPageOutput>> {
    const selectedPage = input?.pageIndex ?? 0;
    const selectedPageSize = input?.pageSize ?? 0;
    if (!Number.isSafeInteger(selectedPage) || !Number.isSafeInteger(selectedPageSize) || selectedPage < 0 || selectedPageSize < 0 || selectedPageSize > 100) return branch('defect', 'VALIDATION');
    const pageIndex = selectedPage || 1;
    const pageSize = selectedPageSize || 10;
    const exchange = await this.client.list(call, pageIndex, pageSize);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { wire = undefined; }
    if (!isObject(wire) || typeof wire.code !== 'number' || !Number.isSafeInteger(wire.code) || typeof wire.success !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    if (!wire.success || wire.code !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!Array.isArray(wire.content) || !isObject(wire.pageInfo)) return branch('invalidResponse', 'PROTOCOL');
    const pageInfo = wire.pageInfo;
    if (typeof pageInfo.count !== 'number' || !Number.isSafeInteger(pageInfo.count) || pageInfo.count !== wire.content.length || pageInfo.count < 0 || pageInfo.pageIndex !== pageIndex || typeof pageInfo.pageSize !== 'number' || !Number.isSafeInteger(pageInfo.pageSize) || pageInfo.pageSize < 1 || pageInfo.pageSize > 100 || typeof pageInfo.totalCount !== 'number' || !Number.isSafeInteger(pageInfo.totalCount) || pageInfo.totalCount < pageInfo.count) return branch('invalidResponse', 'PROTOCOL');
    const dashboards: Array<{ uuid: string; name: string }> = [];
    for (const dashboard of wire.content) {
      if (!isObject(dashboard) || typeof dashboard.uuid !== 'string' || !dashboard.uuid || typeof dashboard.name !== 'string' || !dashboard.name) return branch('invalidResponse', 'PROTOCOL');
      dashboards.push({ uuid: dashboard.uuid, name: dashboard.name });
    }
    return newQueryBranch('listed', { totalCount: pageInfo.totalCount, pageIndex: pageInfo.pageIndex as number, pageSize: pageInfo.pageSize, dashboards });
  }
}

function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'guance', operation: 'listDashboardsPage', kind, message: 'Guance dashboard query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListDashboardsPageOutput> { return newQueryBranch(name, { totalCount: 0, pageIndex: 0, pageSize: 0, dashboards: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListDashboardsPageOutput> { return newQueryRetry(failure(kind), delay); }
