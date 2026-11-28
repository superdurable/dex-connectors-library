// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { LookupIPLocationDefinition } from './generated/definitions.js';
import type { Config, Credentials, LookupIPLocationInput, LookupIPLocationOutput } from './generated/models.js';

const emptyOutput: LookupIPLocationOutput = { country: '', province: '', city: '', district: '', operator: '' };
const endpoint = 'https://api.ipplus360.com/ip/geo/v1/district/';

/** Optional provider transport used for deterministic tests. */
export interface IPPlus360ClientOptions {
  /** Sender; production uses the Connector SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Resolve one secret token per read-only IP location Query. */
export class IPPlus360Client {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate request limits without contacting IPPlus360. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: IPPlus360ClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('IPPlus360 configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('IPPlus360 request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the location lookup Query. */ lookupIPLocation(): LookupIPLocationOperation { return new LookupIPLocationOperation(this); }

  /** Dispatch one authenticated provider request. */
  async request(call: Call, ipAddress: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let token = '';
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = resolved.apiKey instanceof SecretString ? resolved.apiKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!token || token.length > 1024 || !/^[!-~]+$/.test(token)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${endpoint}?ip=${encodeURIComponent(ipAddress)}&key=${encodeURIComponent(token)}`, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? '') };
  }
}

/** Query coarse IP location fields through IPPlus360's v2 API. */
export class LookupIPLocationOperation implements Query<LookupIPLocationInput, LookupIPLocationOutput> {
  /** Bind a validated IPPlus360 client. */ constructor(private readonly client: IPPlus360Client) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return LookupIPLocationDefinition; }
  /** Classify a provider attempt and preserve secret safety. */
  async invoke(call: Call, input: LookupIPLocationInput): Promise<QueryAttempt<LookupIPLocationOutput>> {
    const address = publicIPv4(input?.ipAddress);
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
    try { wire = fieldsByName(JSON.parse(new TextDecoder().decode(body))); }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire) return branch('invalidResponse', 'PROTOCOL');
    const code = wire.code;
    if (typeof code !== 'string' || !code) return branch('invalidResponse', 'PROTOCOL');
    const values = wire.data == null ? {} : fieldsByName(wire.data);
    if (!values) return branch('invalidResponse', 'PROTOCOL');
    if (['country', 'prov', 'city', 'district', 'isp'].some(field => values[field] != null && typeof values[field] !== 'string')) return branch('invalidResponse', 'PROTOCOL');
    if (code === 'NoData') return branch('providerRejected', 'NOT_FOUND');
    if (code !== 'Success') {
      const kind: Failure['kind'] = ['KeyNotFound', 'KeyExpired'].includes(code) ? 'AUTHENTICATION' : ['KeyUnauthorized', 'KeyDisabled'].includes(code) ? 'AUTHORIZATION' : ['KeyNotEnoughBalance', 'DailyLimited'].includes(code) ? 'QUOTA_EXHAUSTED' : 'PROVIDER_REJECTION';
      return branch('providerRejected', kind);
    }
    if (typeof values.country !== 'string' || !values.country.trim() || typeof values.city !== 'string' || !values.city.trim() || typeof values.district !== 'string' || !values.district.trim()) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { country: values.country, city: values.city, province: String(values.prov ?? ''), district: values.district, operator: String(values.isp ?? '') });
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
function failure(kind: Failure['kind']): Failure { return { provider: 'ipplus360', operation: 'lookupIPLocation', kind, message: 'IPPlus360 location lookup failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<LookupIPLocationOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<LookupIPLocationOutput> { return newQueryRetry(failure(kind), delay); }

function fieldsByName(value: unknown): Record<string, unknown> | undefined {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined;
  return Object.fromEntries(Object.entries(value).map(([name, field]) => [name.toLowerCase(), field]));
}
