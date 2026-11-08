// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, ReauthorizationRequiredError, resolveCredential, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type CredentialRefreshDriver, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListSitesDefinition } from './generated/definitions.js';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials, ListSitesInput, Site, SitesPage } from './generated/models.js';

const emptySites = (): SitesPage => ({ sites: [] });
const validPermissions = new Set(['siteOwner', 'siteFullUser', 'siteRestrictedUser', 'siteUnverifiedUser']);

/** Injectable transport and OAuth refresh dependencies. */
export interface SearchConsoleClientOptions {
  /** Provider sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Refresh driver; omitted uses the manifest declaration. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Clock used to interpret HTTP Retry-After dates. */ now?: () => Date;
}

/** One bounded Search Console exchange. */
export interface SearchConsoleExchange {
  /** Provider HTTP status, or zero before response headers. */ status: number;
  /** Bounded response body retained only in process memory. */ body: Uint8Array;
  /** Provider Retry-After delay in milliseconds. */ retryAfterMs: number;
  /** Safe local error class. */ error?: 'credential' | 'refresh' | 'request' | 'tooLarge';
}

/** Search Console client with call-time OAuth resolution and bounded responses. */
export class SearchConsoleClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly refreshDriver: CredentialRefreshDriver<Credentials>;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate configuration without contacting Google. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SearchConsoleClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Search Console configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://www.googleapis.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Search Console request limits are invalid');
    this.refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Google Search Console', auth: Authorization, credentialFields: CredentialFields,
    });
    this.sendRequest = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the site listing Query. */ listSites(): ListSitesOperation { return new ListSitesOperation(this); }

  /** Read one bounded provider response for a Query. */
  async get(call: Call, path: string): Promise<SearchConsoleExchange> {
    let token: string;
    try {
      const resolved = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      token = resolved.accessToken instanceof SecretString ? resolved.accessToken.reveal() : '';
      if (!token || !isHeaderSafeCredential(token)) return failedExchange('credential');
    } catch (error) {
      return failedExchange(error instanceof ReauthorizationRequiredError ? 'credential' : 'refresh');
    }
    const request: ProviderRequest = {
      url: this.baseURL + path, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${token}`, 'User-Agent': 'Dex Connector (https://superdurable.io)' },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { ...failedExchange('tooLarge'), status: error.status, retryAfterMs: parseRetryAfter(header(error.headers ?? {}, 'retry-after'), this.now()) };
      if (error instanceof ProviderTransportError) return failedExchange('request');
      return failedExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'), this.now());
    if (Buffer.from(response.body).includes(token)) return { ...failedExchange('credential'), status: response.status, retryAfterMs };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Read properties visible to the connected Google account. */
export class ListSitesOperation implements Query<ListSitesInput, SitesPage> {
  /** Bind the provider client. */ constructor(private readonly client: SearchConsoleClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListSitesDefinition; }
  /** Project one bounded provider response. */
  async invoke(call: Call, input: ListSitesInput): Promise<QueryAttempt<SitesPage>> {
    if (!input || typeof input !== 'object') return queryBranch('defect', 'VALIDATION', 'site listing input is invalid');
    const exchange = await this.client.get(call, '/webmasters/v3/sites');
    if (exchange.error === 'credential') return queryBranch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    if (exchange.error === 'request' || exchange.error === 'refresh') return queryRetry('TRANSPORT', 'Search Console request failed');
    if (exchange.status === 429) return queryRetry('RATE_LIMIT', 'Google rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return queryRetry('AVAILABILITY', 'Search Console is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return queryBranch('providerRejected', 'PROVIDER_REJECTION', `Google rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return queryBranch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Search Console response exceeded the configured limit');
    const document = parseJSON(exchange.body);
    if (!isObject(document)) return queryBranch('invalidResponse', 'PROTOCOL', 'Google returned invalid property JSON');
    const entries = document.siteEntry ?? [];
    if (!Array.isArray(entries) || entries.length > 1000) return queryBranch('invalidResponse', 'PROTOCOL', 'Google returned invalid property JSON');
    const sites: Site[] = [];
    for (const entry of entries) {
      if (!isObject(entry) || typeof entry.siteUrl !== 'string' || !entry.siteUrl || typeof entry.permissionLevel !== 'string' || !validPermissions.has(entry.permissionLevel)) return queryBranch('invalidResponse', 'PROTOCOL', 'Google returned incomplete property metadata');
      sites.push({ siteUrl: entry.siteUrl, permissionLevel: entry.permissionLevel });
    }
    return newQueryBranch('listed', { sites });
  }
}

function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'google-search-console', operation: 'listSites', kind, message }; }
function queryBranch(branch: string, kind: Failure['kind'], message: string): QueryAttempt<SitesPage> { return newQueryBranch(branch, emptySites(), {}, failure(kind, message)); }
function queryRetry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<SitesPage> { return newQueryRetry(failure(kind, message), delay); }
function failedExchange(error: NonNullable<SearchConsoleExchange['error']>): SearchConsoleExchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
