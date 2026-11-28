// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
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
    const address = canonicalAddress(input?.ipAddress);
    if (!address) return branch('defect', 'VALIDATION');
    const exchange = await this.client.request(call, address);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: Record<string, unknown> | undefined;
    try { wire = fieldsByName(JSON.parse(new TextDecoder().decode(body), readProviderNumber)); }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire) return branch('invalidResponse', 'PROTOCOL');
    if (!Number.isInteger(wire.status)) return branch('invalidResponse', 'PROTOCOL');
    const data = wire.data == null ? {} : fieldsByName(wire.data);
    if (!data || ['prov','city','adcode'].some(field => data[field] != null && typeof data[field] !== 'string')) return branch('invalidResponse', 'PROTOCOL');
    if (wire.status === 1008 || wire.status === 1010) return retry('RATE_LIMIT');
    if (wire.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    const province = String(data.prov ?? ''), city = String(data.city ?? ''), adcode = String(data.adcode ?? '');
    const location = province + city + adcode;
    if (wire.data == null || Buffer.byteLength(province) > 128 || Buffer.byteLength(city) > 128 || Buffer.byteLength(adcode) > 16 || /[\r\n\0]/u.test(location) || location.includes(exchange.ak!)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', {province, city, adcode});
  }
}
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'navinfo', operation: 'lookupIPLocation', kind, message: 'NavInfo IP location query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<LookupIPLocationOutput> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<LookupIPLocationOutput> { return newQueryRetry(failure(kind), delay); }

function canonicalAddress(value: unknown): string | undefined {
  if (typeof value !== 'string' || value.includes('%')) return undefined;
  const version = isIP(value);
  if (version === 4) return value;
  if (version !== 6) return undefined;
  const address = new URL('http://[' + value + ']').hostname.slice(1, -1);
  const mapped = /^::ffff:([a-f0-9]+):([a-f0-9]+)$/.exec(address);
  if (!mapped) return undefined;
  const high = parseInt(mapped[1]!, 16), low = parseInt(mapped[2]!, 16);
  return [high >>> 8, high & 255, low >>> 8, low & 255].join('.');
}

function fieldsByName(value: unknown): Record<string, unknown> | undefined {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined;
  return Object.fromEntries(Object.entries(value).map(([name, field]) => [name.toLowerCase(), field]));
}

// Go decodes the result code into int64 before classifying the provider response.
function readProviderNumber(key: string, value: unknown, context?: { source?: string }): unknown {
  if (key.toLowerCase() !== 'status' || typeof value !== 'number') return value;
  const source = context?.source;
  if (source === undefined) return Number.isSafeInteger(value) ? value : null;
  if (!/^-?\d+$/.test(source) || source.length > 20) return null;
  const integer = BigInt(source);
  return integer >= -(1n << 63n) && integer < (1n << 63n) ? value : null;
}
