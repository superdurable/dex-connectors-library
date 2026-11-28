// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
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
    const ipAddress = publicIPv4(input?.ipAddress);
    if (!ipAddress) return branch('defect', 'VALIDATION');
    const exchange = await this.client.getLocation(call, ipAddress);
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
    const code = wire.code;
    if (typeof code !== 'number' || !Number.isInteger(code)) return branch('invalidResponse', 'PROTOCOL');
    const data = wire.data;
    wire = data == null ? {} : fieldsByName(data);
    if (!wire) return branch('invalidResponse', 'PROTOCOL');
    const values = wire;
    if (['country', 'province', 'city', 'district', 'isp'].some(field => values[field] != null && typeof values[field] !== 'string')) return branch('invalidResponse', 'PROTOCOL');

    if (code === 10006 || code === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (code >= 500 && code <= 599) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (code !== 200) return branch('providerRejected', code === 10001 || code === 10002 ? 'AUTHENTICATION' : code === 10003 ? 'AUTHORIZATION' : code === 10005 ? 'QUOTA_EXHAUSTED' : 'PROVIDER_REJECTION');
    if (typeof values.country !== 'string' || !values.country.trim() || typeof values.city !== 'string' || !values.city.trim()) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { country: values.country, city: values.city, province: String(values.province ?? ''), district: String(values.district ?? ''), operator: String(values.isp ?? '') });
  }
}

function publicIPv4(value: unknown): string | undefined {
  if (typeof value !== 'string' || value.includes('%')) return undefined;
  const version = isIP(value);
  let address = value;
  if (version === 6) {
    const mapped = /^\[::ffff:([a-f0-9]+):([a-f0-9]+)\]$/.exec(new URL('http://[' + value + ']').hostname);
    if (!mapped) return undefined;
    const high = parseInt(mapped[1]!, 16);
    const low = parseInt(mapped[2]!, 16);
    address = [high >>> 8, high & 255, low >>> 8, low & 255].join('.');
  } else if (version !== 4) return undefined;
  const parts = address.split('.');
  const first = Number(parts[0]);
  const second = Number(parts[1]);
  return !(parts.every(part => part === '0')) && first !== 10 && first !== 127 && !(first >= 224 && first <= 239) && !(first === 169 && second === 254) && !(first === 172 && second >= 16 && second <= 31) && !(first === 192 && second === 168) ? address : undefined;
}

function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'haimou', operation: 'lookupIPLocation', kind, message: 'Haimou IP location query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<LookupIPLocationOutput> { return newQueryBranch(name, emptyLocation, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<LookupIPLocationOutput> { return newQueryRetry(failure(kind), delay); }

function fieldsByName(value: unknown): Record<string, unknown> | undefined {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined;
  return Object.fromEntries(Object.entries(value).map(([name, field]) => [name.toLowerCase(), field]));
}

// Go decodes the result code into int64 before classifying the provider response.
function readProviderNumber(key: string, value: unknown, context?: { source?: string }): unknown {
  if (key.toLowerCase() !== 'code' || typeof value !== 'number') return value;
  const source = context?.source;
  if (source === undefined) return Number.isSafeInteger(value) ? value : null;
  if (!/^-?\d+$/.test(source) || source.length > 20) return null;
  const integer = BigInt(source);
  return integer >= -(1n << 63n) && integer < (1n << 63n) ? value : null;
}
