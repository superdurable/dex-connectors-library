// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SearchPartnersDefinition } from './generated/definitions.js';
import type { Config, Credentials, Partner, PartnerSearchPage, SearchPartnersInput } from './generated/models.js';

const emptyPage = (): PartnerSearchPage => ({ partners: [] });

/** Injectable provider sender for tests and local applications. */
export interface OdooClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Odoo JSON-2 client for company partner search. */
export class OdooClient {
  private readonly baseURL: string;
  private readonly database: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate JSON-2 settings without contacting Odoo. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: OdooClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Odoo configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl).replace(/\/$/, '');
    if (new URL(this.baseURL).pathname !== '/') throw new TypeError('Odoo base URL must be an origin without a path');
    this.database = config.database;
    if (!isHeaderSafeCredential(this.database) || this.database.length > 128) throw new TypeError('Odoo database name is invalid');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Odoo request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the searchPartners Query. */ searchPartners(): SearchPartnersOperation { return new SearchPartnersOperation(this); }

  /** Resolve the API key and send one bounded JSON-2 search. */
  async search(call: Call, body: Uint8Array): Promise<Exchange> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!isHeaderSafeCredential(apiKey)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/json/2/res.partner/search_read`, method: 'POST',
      headers: { Authorization: `Bearer ${apiKey}`, 'X-Odoo-Database': this.database, 'Content-Type': 'application/json', Accept: 'application/json' },
      body, maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
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

/** Search company partners by Odoo ilike name pattern. */
export class SearchPartnersOperation implements Query<SearchPartnersInput, PartnerSearchPage> {
  /** Bind one Odoo client. */ constructor(private readonly client: OdooClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return SearchPartnersDefinition; }
  /** Search once and classify provider outcomes consistently with Go. */
  async invoke(call: Call, input: SearchPartnersInput): Promise<QueryAttempt<PartnerSearchPage>> {
    if (!isValidNamePattern(input?.namePattern) || !Number.isSafeInteger(input.limit ?? 0) || (input.limit ?? 0) < 0 || (input.limit ?? 0) > 100)
      return terminal('defect', 'VALIDATION', 'name pattern or limit is invalid');
    const limit = input.limit || 20;
    const body = Buffer.from(JSON.stringify({ domain: [['is_company', '=', true], ['name', 'ilike', input.namePattern]], fields: ['id', 'name'], limit }));
    const exchange = await this.client.search(call, body);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Odoo API key is unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Odoo request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Odoo rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Odoo is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Odoo rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Odoo response exceeded the configured limit');
    const page = decodePartners(exchange.body, limit);
    if (!page) return terminal('invalidResponse', 'PROTOCOL', 'Odoo returned invalid partner data');
    return newQueryBranch('found', page);
  }
}

function isValidNamePattern(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value.trim() === value &&
    new TextEncoder().encode(value).length <= 256 && ![0, 10, 13].some(code => value.includes(String.fromCharCode(code))) &&
    !/(?:[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF])/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'odoo', operation: 'searchPartners', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<PartnerSearchPage> { return newQueryBranch(branch, emptyPage(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<PartnerSearchPage> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodePartners(body: Uint8Array, limit: number): PartnerSearchPage | undefined {
  let raw: unknown;
  try { raw = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; }
  if (!Array.isArray(raw) || raw.length > limit) return undefined;
  const partners: Partner[] = [];
  for (const item of raw) {
    if (!isObject(item) || !Number.isSafeInteger(item.id) || (item.id as number) <= 0 || typeof item.name !== 'string' || !item.name) return undefined;
    partners.push({ id: item.id as number, name: item.name });
  }
  return { partners };
}
