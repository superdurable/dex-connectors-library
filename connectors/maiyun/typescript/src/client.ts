// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { LookupIPLocationDefinition } from './generated/definitions.js';
import type { Config, Credentials, LookupIPLocationInput, LookupIPLocationOutput } from './generated/models.js';

const endpoint = 'https://lts.maiyun.net/api/service/ip';
const emptyOutput: LookupIPLocationOutput = { country: '', province: '', city: '', district: '' };
/** Optional HTTP sender for deterministic provider tests. */
export interface MaiyunClientOptions { /** Sender; defaults to the Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Read the IP location with private credentials resolved per call. */
export class MaiyunClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate local settings without contacting Maiyun. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: MaiyunClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Maiyun configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Maiyun request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only IP-location Query. */ lookupIPLocation(): LookupIPLocationOperation { return new LookupIPLocationOperation(this); }
  /** Dispatch one bounded provider request. */
  async request(call: Call, input: LookupIPLocationInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const apiKey = credentials.token instanceof SecretString ? credentials.token.reveal() : '';
    if (!/^[!-~]+$/.test(apiKey) || apiKey.length > 1024) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: endpoint, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json', Authorization: `Bearer ${apiKey}` }, body: new TextEncoder().encode(JSON.stringify({ ip: input.ipAddress })), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
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
/** Classify Maiyun IP-location results without retaining secrets or metadata. */
export class LookupIPLocationOperation implements Query<LookupIPLocationInput, LookupIPLocationOutput> {
  /** Bind one validated client. */ constructor(private readonly client: MaiyunClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return LookupIPLocationDefinition; }
  /** Read and validate the IP location. */
  async invoke(call: Call, input: LookupIPLocationInput): Promise<QueryAttempt<LookupIPLocationOutput>> {
    const ipAddress = publicIPv4(input?.ipAddress);
    if (!ipAddress) return branch('defect', 'VALIDATION');
    const exchange = await this.client.request(call, { ipAddress });
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
    const code = wire.result;
    if (typeof code !== 'number' || !Number.isInteger(code)) return branch('invalidResponse', 'PROTOCOL');
    const values = wire;
    if (['country', 'province', 'city', 'district'].some(field => values[field] != null && typeof values[field] !== 'string')) return branch('invalidResponse', 'PROTOCOL');

    if (code === -429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (code <= -500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (code !== 1) return branch('providerRejected', code === -401 || code === -402 ? 'AUTHENTICATION' : code === -403 ? 'AUTHORIZATION' : code === -10 ? 'QUOTA_EXHAUSTED' : 'PROVIDER_REJECTION');
    if (typeof values.country !== 'string' || !values.country.trim() || typeof values.city !== 'string' || !values.city.trim()) return branch('invalidResponse', 'PROTOCOL');
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

function failure(kind: Failure['kind']): Failure { return { provider: 'maiyun', operation: 'lookupIPLocation', kind, message: 'Maiyun provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<LookupIPLocationOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<LookupIPLocationOutput> { return newQueryRetry(failure(kind), delay); }

function fieldsByName(value: unknown): Record<string, unknown> | undefined {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined;
  return Object.fromEntries(Object.entries(value).map(([name, field]) => [name.toLowerCase(), field]));
}

// Go decodes the result code into int64 before classifying the provider response.
function readProviderNumber(key: string, value: unknown, context?: { source?: string }): unknown {
  if (key.toLowerCase() !== 'result' || typeof value !== 'number') return value;
  const source = context?.source;
  if (source === undefined) return Number.isSafeInteger(value) ? value : null;
  if (!/^-?\d+$/.test(source) || source.length > 20) return null;
  const integer = BigInt(source);
  return integer >= -(1n << 63n) && integer < (1n << 63n) ? value : null;
}
