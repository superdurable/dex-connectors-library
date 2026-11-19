// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { LookupIPLocationDefinition } from './generated/definitions.js';
import type { Config, Credentials, LookupIPLocationInput, LookupIPLocationOutput } from './generated/models.js';

/** Optional HTTP transport for local provider tests. */
export interface HaimouClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
const emptyLocation: LookupIPLocationOutput = { country: '', province: '', city: '', district: '', operator: '' };

/** Resolve Haimou credentials for each public IPv4 lookup. */
export class HaimouClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings without contacting Haimou. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: HaimouClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Haimou configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('Haimou request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only IP location Query. */ lookupIPLocation(): LookupIPLocationOperation { return new LookupIPLocationOperation(this); }
  /** Send one bounded IP lookup request without exposing its query-string credential. */
  async getLocation(call: Call, ipAddress: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try { const credential = await this.credentials.resolve(call, call.context.cancellationSignal); apiKey = credential.apiKey instanceof SecretString ? credential.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey || apiKey.length > 1024 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const url = new URL('https://hmkj4399.com/v1/ip');
    url.searchParams.set('apikey', apiKey);
    url.searchParams.set('ip', ipAddress);
    const request: ProviderRequest = { url: url.toString(), method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Look up coarse location and network information for a public IPv4 address. */
export class LookupIPLocationOperation implements Query<LookupIPLocationInput, LookupIPLocationOutput> {
  /** Bind the provider client. */ constructor(private readonly client: HaimouClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return LookupIPLocationDefinition; }
  /** Classify one provider response using the Go connector contract. */
  async invoke(call: Call, input: LookupIPLocationInput): Promise<QueryAttempt<LookupIPLocationOutput>> {
    const ipAddress = input?.ipAddress;
    if (typeof ipAddress !== 'string' || !isPublicIPv4(ipAddress)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.getLocation(call, ipAddress);
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
    const result = wire as { code?: unknown; data?: unknown };
    if (!Number.isInteger(result.code)) return branch('invalidResponse', 'PROTOCOL');
    if (result.code === 10006 || result.code === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (typeof result.code === 'number' && result.code >= 500 && result.code <= 599) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (result.code !== 200) return branch('providerRejected', result.code === 10001 || result.code === 10002 ? 'AUTHENTICATION' : result.code === 10003 ? 'AUTHORIZATION' : result.code === 10005 ? 'QUOTA_EXHAUSTED' : 'PROVIDER_REJECTION');
    if (!result.data || typeof result.data !== 'object' || Array.isArray(result.data)) return branch('invalidResponse', 'PROTOCOL');
    const data = result.data as Record<string, unknown>;
    if (typeof data.country !== 'string' || !data.country.trim() || typeof data.city !== 'string' || !data.city.trim()) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { country: data.country, province: typeof data.province === 'string' ? data.province : '', city: data.city, district: typeof data.district === 'string' ? data.district : '', operator: typeof data.isp === 'string' ? data.isp : '' });
  }
}

function isPublicIPv4(address: string): boolean {
  const parts = address.split('.');
  if (parts.length !== 4 || parts.some(part => !/^(0|[1-9]\d{0,2})$/.test(part) || Number(part) > 255)) return false;
  const [first = 0, second = 0] = parts.map(Number);
  return first !== 0 && first !== 10 && first !== 127 && first !== 255 && !(first === 169 && second === 254) && !(first === 172 && second >= 16 && second <= 31) && !(first === 192 && second === 168) && first < 224;
}
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'haimou', operation: 'lookupIPLocation', kind, message: 'Haimou IP location query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<LookupIPLocationOutput> { return newQueryBranch(name, emptyLocation, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<LookupIPLocationOutput> { return newQueryRetry(failure(kind), delay); }
