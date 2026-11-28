// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
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
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
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
    const address = publicIPv4(input?.ipAddress);
    if (!address) return branch('defect', 'VALIDATION');
    let appID = ''; let appSecret = '';
    try {
      const fields = await this.client.credentials.resolve(call, call.context.cancellationSignal);
      appID = fields.appID instanceof SecretString ? fields.appID.reveal() : '';
      appSecret = fields.appSecret instanceof SecretString ? fields.appSecret.reveal() : '';
    } catch { return branch('defect', 'AUTHENTICATION'); }
    if (!isValidCredential(appID) || !isValidCredential(appSecret)) return branch('defect', 'AUTHENTICATION');
    const url = new URL(endpoint); url.searchParams.set('appid', appID); url.searchParams.set('appsecret', appSecret); url.searchParams.set('ip', address);
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
    let wire: Record<string, unknown> | undefined;
    try { wire = fieldsByName(JSON.parse(new TextDecoder().decode(response.body), readProviderNumber)); }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire) return branch('invalidResponse', 'PROTOCOL');
    const code = wire.errcode;
    if (typeof code !== 'number' || !Number.isInteger(code)) return branch('invalidResponse', 'PROTOCOL');
    const values = wire;
    if (['ip', 'country', 'province', 'city', 'district'].some(field => values[field] != null && typeof values[field] !== 'string')) return branch('invalidResponse', 'PROTOCOL');
    if (code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (typeof values.country !== 'string' || !values.country.trim() || typeof values.city !== 'string' || !values.city.trim() || values.ip !== address) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { country: values.country, city: values.city, province: String(values.province ?? ''), district: String(values.district ?? '') });
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
function isValidCredential(value: string): boolean { return value.length > 0 && value.length <= 1024 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'yikeapi', operation: 'lookupIPLocation', kind, message: 'YikeAPI location lookup failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<LookupIPLocationOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<LookupIPLocationOutput> { return newQueryRetry(failure(kind), delay); }

function fieldsByName(value: unknown): Record<string, unknown> | undefined {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined;
  return Object.fromEntries(Object.entries(value).map(([name, field]) => [name.toLowerCase(), field]));
}


// Go decodes the result code into int64 before classifying the provider response.
function readProviderNumber(key: string, value: unknown, context?: { source?: string }): unknown {
  if (key.toLowerCase() !== 'errcode' || typeof value !== 'number') return value;
  const source = context?.source;
  if (source === undefined) return Number.isSafeInteger(value) ? value : null;
  if (!/^-?\d+$/.test(source) || source.length > 20) return null;
  const integer = BigInt(source);
  return integer >= -(1n << 63n) && integer < (1n << 63n) ? value : null;
}
