// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { LookupIPLocationDefinition } from './generated/definitions.js';
import type { Config, Credentials, LookupIPLocationInput, LookupIPLocationOutput } from './generated/models.js';

/** Optional HTTP transport for provider tests. */
export interface NavInfoClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
const empty: LookupIPLocationOutput = { province: '', city: '', adcode: '' };
/** Resolve NavInfo credentials for each city-level IP query. */
export class NavInfoClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate settings without contacting NavInfo. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: NavInfoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('NavInfo settings and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('NavInfo limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the city-level location Query. */ lookupIPLocation(): LookupIPLocationOperation { return new LookupIPLocationOperation(this); }
  /** Send one bounded request without exposing the query-string credential. */
  async request(call: Call, ipAddress: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number; ak?: string }> {
    let ak = '';
    try { const resolved = await this.credentials.resolve(call, call.context.cancellationSignal); ak = resolved.ak instanceof SecretString ? resolved.ak.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!ak || ak.length > 1024 || /[^\x21-\x7e]/.test(ak)) return { error: 'credential', retryAfterMs: 0 };
    const url = new URL('https://lbs.navinfo.com/open-api/position/v1/ip');
    url.searchParams.set('ak', ak); url.searchParams.set('ip', ipAddress);
    const request: ProviderRequest = { url: url.toString(), method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0, ak };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')), ak };
  }
}
/** Look up coarse location for one IPv4 address. */
export class LookupIPLocationOperation implements Query<LookupIPLocationInput, LookupIPLocationOutput> {
  /** Bind the provider client. */ constructor(private readonly client: NavInfoClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return LookupIPLocationDefinition; }
  /** Classify the provider response using the Go contract. */
  async invoke(call: Call, input: LookupIPLocationInput): Promise<QueryAttempt<LookupIPLocationOutput>> {
    const address = input?.ipAddress;
    if (typeof address !== 'string' || !isIPv4(address)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.request(call, address);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const record = wire as Record<string, unknown>;
    if (!Number.isInteger(record.status)) return branch('invalidResponse', 'PROTOCOL');
    if (record.status === 1008 || record.status === 1010) return retry('RATE_LIMIT');
    if (record.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!record.data || typeof record.data !== 'object' || Array.isArray(record.data)) return branch('invalidResponse', 'PROTOCOL');
    const data = record.data as Record<string, unknown>;
    const output = { province: data.prov, city: data.city, adcode: data.adcode };
    if (Object.values(output).some(value => typeof value !== 'string') || String(output.province).length > 128 || String(output.city).length > 128 || String(output.adcode).length > 16 || Object.values(output).join('').split('').some(character => character.charCodeAt(0) < 32) || Object.values(output).join('').includes(exchange.ak ?? '')) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', output as LookupIPLocationOutput);
  }
}
function isIPv4(value: string): boolean { const octets = value.split('.'); return octets.length === 4 && octets.every(octet => /^(0|[1-9]\d{0,2})$/.test(octet) && Number(octet) <= 255); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'navinfo', operation: 'lookupIPLocation', kind, message: 'NavInfo IP location query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<LookupIPLocationOutput> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<LookupIPLocationOutput> { return newQueryRetry(failure(kind), delay); }
