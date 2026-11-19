// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { LookupIPLocationDefinition } from './generated/definitions.js';
import type { Config, Credentials, LookupIPLocationInput, LookupIPLocationOutput } from './generated/models.js';

const endpoint = 'https://apia.yikeapi.com/ip/';
const emptyOutput: LookupIPLocationOutput = { country: '', province: '', city: '', district: '' };

/** Optional provider sender for deterministic tests. */
export interface YikeAPIClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve credentials for each read-only IP location Query. */
export class YikeAPIClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate request limits without contacting the provider. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: YikeAPIClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('YikeAPI configuration and credentials are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('YikeAPI request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the IP location Query. */
  lookupIPLocation(): LookupIPLocationOperation { return new LookupIPLocationOperation(this); }
}

/** Read coarse location fields for a public IPv4 address. */
export class LookupIPLocationOperation implements Query<LookupIPLocationInput, LookupIPLocationOutput> {
  /** Bind a validated provider client. */ constructor(private readonly client: YikeAPIClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return LookupIPLocationDefinition; }
  /** Classify a provider attempt without exposing credentials. */
  async invoke(call: Call, input: LookupIPLocationInput): Promise<QueryAttempt<LookupIPLocationOutput>> {
    if (!input || !isPublicIPv4(input.ipAddress)) return branch('defect', 'VALIDATION');
    let appID = ''; let appSecret = '';
    try {
      const fields = await this.client.credentials.resolve(call, call.context.cancellationSignal);
      appID = fields.appID instanceof SecretString ? fields.appID.reveal() : '';
      appSecret = fields.appSecret instanceof SecretString ? fields.appSecret.reveal() : '';
    } catch { return branch('defect', 'AUTHENTICATION'); }
    if (!isValidCredential(appID) || !isValidCredential(appSecret)) return branch('defect', 'AUTHENTICATION');
    const url = new URL(endpoint); url.searchParams.set('appid', appID); url.searchParams.set('appsecret', appSecret); url.searchParams.set('ip', input.ipAddress);
    let response: ProviderResponse; let tooLarge = false;
    try {
      response = await this.client.send({ url: url.toString(), method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (!(error instanceof BodyTooLargeError) || error.status === undefined) return retry('AVAILABILITY');
      response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }; tooLarge = true;
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    const retryAfterMs = parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', retryAfterMs);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', retryAfterMs);
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (tooLarge || response.body.length > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || !Number.isInteger(document.errcode)) return branch('invalidResponse', 'PROTOCOL');
    if (document.errcode !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (document.ip !== input.ipAddress || typeof document.country !== 'string' || !document.country.trim() || typeof document.province !== 'string' || typeof document.city !== 'string' || !document.city.trim() || typeof document.district !== 'string') return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { country: document.country, province: document.province, city: document.city, district: document.district });
  }
}

function isPublicIPv4(value: unknown): value is string {
  if (typeof value !== 'string') return false;
  const parts = value.split('.');
  if (parts.length !== 4 || parts.some(part => !/^(0|[1-9]\d{0,2})$/.test(part) || Number(part) > 255)) return false;
  const first = Number(parts[0]); const second = Number(parts[1]);
  return first !== 0 && first !== 10 && first !== 127 && first < 224 && !(first === 169 && second === 254) && !(first === 172 && second >= 16 && second <= 31) && !(first === 192 && second === 168);
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isValidCredential(value: string): boolean { return value.length > 0 && value.length <= 512 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'yikeapi', operation: 'lookupIPLocation', kind, message: 'YikeAPI location lookup failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<LookupIPLocationOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<LookupIPLocationOutput> { return newQueryRetry(failure(kind), delay); }
